package recommend

import (
	"math"
	"sort"
	"strings"

	"github.com/rhymeswithlimo/northrou/backend/internal/db"
)

// Content vectors. Every title becomes a sparse TF-IDF vector over a vocabulary
// of normalized keyword tokens plus a lighter genre backbone. Cosine proximity
// in this space means shared thematic/tonal territory, not just shared genre -
// which is the whole point of the upgrade. Pure Go, deterministic, no SVD and no
// external dependencies; on a single household's library (hundreds to a few
// thousand titles) it builds in well under a second.

const (
	// genreTokenWeight scales genre tokens relative to keyword tokens. Keywords
	// are the fine thematic signal; genres are a coarse backbone that keeps a
	// vector meaningful when a title has few or no keywords.
	genreTokenWeight = 0.5

	// minKeywordDF: a keyword must appear on at least this many titles to enter
	// the vocabulary. A keyword on a single title cannot link two distinct
	// titles and would carry maximal IDF, so it only adds noise. This is an
	// absolute floor (not size-relative) because "connects >= 2 titles" is the
	// hard threshold for a co-occurrence signal to exist at all.
	minKeywordDF = 2

	// maxKeywordDFFrac drops keywords appearing on more than this fraction of the
	// catalog: too common to discriminate. Relative to library size, as it must
	// be - "on 30% of a 3000-title library" and "of a 300-title one" differ.
	maxKeywordDFFrac = 0.5
)

type titleKey struct {
	kind string // "movie" | "show"
	id   int64
}

func movieKey(id int64) titleKey { return titleKey{"movie", id} }
func showKey(id int64) titleKey  { return titleKey{"show", id} }

// sparseVec maps a vocabulary index to a weight. Vectors stored in a
// vectorSpace are L2-normalized, so cosine similarity is a plain dot product.
type sparseVec map[int]float64

// vectorSpace holds content vectors for the whole library plus the normalized
// keyword set per title (kept for building human "why" copy).
type vectorSpace struct {
	vecs map[titleKey]sparseVec
	kw   map[titleKey]map[string]struct{} // normalized keywords per title
}

// doc is a title's token bag during a build.
type doc struct {
	key    titleKey
	kw     []string // normalized, deduped keyword tokens
	genres []string
}

// buildVectorSpace constructs the TF-IDF content space from library features.
func buildVectorSpace(movies []db.MovieFeature, shows []db.ShowFeature) *vectorSpace {
	docs := make([]doc, 0, len(movies)+len(shows))
	kwSets := map[titleKey]map[string]struct{}{}

	addDoc := func(key titleKey, rawKeywords, genres []string) {
		set := map[string]struct{}{}
		var kw []string
		for _, raw := range rawKeywords {
			n := normalizeKeyword(raw)
			if n == "" {
				continue
			}
			if _, ok := set[n]; ok {
				continue
			}
			set[n] = struct{}{}
			kw = append(kw, n)
		}
		docs = append(docs, doc{key: key, kw: kw, genres: genres})
		kwSets[key] = set
	}
	for _, m := range movies {
		addDoc(movieKey(m.ID), m.Keywords, m.Genres)
	}
	for _, s := range shows {
		addDoc(showKey(s.ID), s.Keywords, s.Genres)
	}

	n := len(docs)
	vs := &vectorSpace{vecs: make(map[titleKey]sparseVec, n), kw: kwSets}
	if n == 0 {
		return vs
	}

	// Document frequency per token. Keyword tokens are prefixed "k:", genre
	// tokens "g:", so a keyword and a genre of the same name never collide.
	df := map[string]int{}
	for _, d := range docs {
		for _, k := range d.kw {
			df["k:"+k]++
		}
		gseen := map[string]struct{}{}
		for _, g := range d.genres {
			t := "g:" + strings.ToLower(g)
			if _, ok := gseen[t]; ok {
				continue
			}
			gseen[t] = struct{}{}
			df[t]++
		}
	}

	maxDF := int(float64(n) * maxKeywordDFFrac)
	if maxDF < minKeywordDF {
		maxDF = n // tiny library: keep everything that clears the min floor
	}

	// Surviving tokens and their IDF. Genres are always kept (coarse backbone);
	// keyword tokens must clear the frequency band.
	keep := map[string]bool{}
	for t, c := range df {
		if strings.HasPrefix(t, "g:") || (c >= minKeywordDF && c <= maxDF) {
			keep[t] = true
		}
	}
	idf := make(map[string]float64, len(keep))
	for t := range keep {
		// Smoothed IDF, always positive.
		idf[t] = math.Log(float64(n+1)/float64(df[t]+1)) + 1
	}

	vocab := map[string]int{}
	indexOf := func(t string) int {
		if i, ok := vocab[t]; ok {
			return i
		}
		i := len(vocab)
		vocab[t] = i
		return i
	}

	for _, d := range docs {
		vec := sparseVec{}
		for _, k := range d.kw {
			t := "k:" + k
			if !keep[t] {
				continue
			}
			vec[indexOf(t)] += idf[t]
		}
		gseen := map[string]struct{}{}
		for _, g := range d.genres {
			t := "g:" + strings.ToLower(g)
			if !keep[t] {
				continue
			}
			if _, ok := gseen[t]; ok {
				continue
			}
			gseen[t] = struct{}{}
			vec[indexOf(t)] += genreTokenWeight * idf[t]
		}
		l2Normalize(vec)
		vs.vecs[d.key] = vec
	}
	return vs
}

// l2Normalize scales a vector to unit length in place (no-op if it is empty).
func l2Normalize(v sparseVec) {
	var sum float64
	for _, w := range v {
		sum += w * w
	}
	if sum == 0 {
		return
	}
	norm := math.Sqrt(sum)
	for i := range v {
		v[i] /= norm
	}
}

// cosine returns the cosine similarity of two vectors. Both must be
// L2-normalized (as vectorSpace stores them and taste() returns them), so this
// is a dot product over shared indices.
func cosine(a, b sparseVec) float64 {
	// Iterate the smaller map for speed.
	if len(b) < len(a) {
		a, b = b, a
	}
	var dot float64
	for i, w := range a {
		dot += w * b[i]
	}
	return dot
}

// vecOf returns a title's content vector, or false if it has none (e.g. a title
// with no surviving tokens).
func (vs *vectorSpace) vecOf(k titleKey) (sparseVec, bool) {
	v, ok := vs.vecs[k]
	return v, ok && len(v) > 0
}

// neighbor is a scored candidate produced by neighbors().
type neighbor struct {
	key   titleKey
	score float64
}

// neighbors returns titles of kind `kind` ranked by cosine to target, keeping
// only those `allow` accepts. target itself is always excluded.
func (vs *vectorSpace) neighbors(target sparseVec, kind string, allow func(titleKey) bool) []neighbor {
	var out []neighbor
	for k, v := range vs.vecs {
		if k.kind != kind || len(v) == 0 {
			continue
		}
		if allow != nil && !allow(k) {
			continue
		}
		if s := cosine(target, v); s > 0 {
			out = append(out, neighbor{key: k, score: s})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return out[i].key.id < out[j].key.id // stable
	})
	return out
}

// taste returns a unit-length taste vector: the weighted sum of the given
// titles' content vectors. Weights are signal strengths (recency/completion),
// computed by the caller; non-positive weights are ignored.
func (vs *vectorSpace) taste(weights map[titleKey]float64) sparseVec {
	acc := sparseVec{}
	for k, w := range weights {
		if w <= 0 {
			continue
		}
		v, ok := vs.vecs[k]
		if !ok {
			continue
		}
		for i, val := range v {
			acc[i] += w * val
		}
	}
	l2Normalize(acc)
	return acc
}

// sharedKeywords returns the normalized keywords two titles have in common,
// sorted for deterministic copy.
func (vs *vectorSpace) sharedKeywords(a, b titleKey) []string {
	sa, sb := vs.kw[a], vs.kw[b]
	if len(sa) == 0 || len(sb) == 0 {
		return nil
	}
	// Iterate the smaller set.
	if len(sb) < len(sa) {
		sa, sb = sb, sa
	}
	var out []string
	for k := range sa {
		if _, ok := sb[k]; ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
