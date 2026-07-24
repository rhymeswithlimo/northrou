// The title detail modal: backdrop, metadata, resume, episodes, cast, similar.
//
// The element is built fresh from the <template> on every open and removed on
// close, so only the dialog actually on screen is ever in the DOM or the tab
// order, and per-title surgery (movies have no episode list) can't leak into
// the next title shown.

import { $, $$ } from '../lib/dom.js';
import { runtime, minutes, remaining, progressPct, seasonCount } from '../lib/format.js';
import { posterCard, personCard, row } from './card.js';
import { setImageSrc } from '../api/images.js';

const FOCUSABLE = 'a[href], button:not([disabled]), select, input, [tabindex]:not([tabindex="-1"])';

function renderMeta(el, data) {
    const meta = $('.detail__meta', el);
    const parts = [];
    if (data.rating) parts.push({ text: `★ ${data.rating.toFixed(1)}`, cls: 'detail__score' });
    if (data.year) parts.push({ text: String(data.year) });
    if (data.seasons?.length) parts.push({ text: seasonCount(data.seasons.length) });
    else if (data.runtime) parts.push({ text: runtime(data.runtime) });
    if (data.certification) parts.push({ text: data.certification, cls: 'detail__cert' });
    for (const g of data.genres ?? []) parts.push({ text: g });

    parts.forEach((p, i) => {
        if (i > 0) {
            const sep = document.createElement('span');
            sep.className = 'detail__sep';
            sep.setAttribute('aria-hidden', 'true');
            sep.textContent = '·';
            meta.append(sep);
        }
        const span = document.createElement('span');
        if (p.cls) span.className = p.cls;
        span.textContent = p.text;
        meta.append(span);
    });
}

function renderEpisodes(el, data) {
    const section = $('.episodes', el);

    // Movies have no episode list: drop the section and the rule that would
    // otherwise leave a doubled divider behind.
    if (!data.seasons?.length) {
        $$('hr', el)[0]?.remove();
        section.remove();
        return;
    }

    const select = $('.episodes__season', section);
    for (const s of data.seasons) {
        const opt = document.createElement('option');
        opt.value = String(s.number);
        opt.textContent = `Season ${s.number}`;
        select.append(opt);
    }

    const list = $('.episodes__list', section);
    const paint = (number) => {
        const season = data.seasons.find((s) => s.number === Number(number));
        list.replaceChildren();
        for (const ep of season?.episodes ?? []) {
            const node = $('#tpl-episode').content.firstElementChild.cloneNode(true);
            const img = $('img', node);
            setImageSrc(img, ep.still_url);
            img.alt = ep.title;
            $('.progress__bar', node).style.width = `${progressPct(ep.position_sec, ep.duration_sec)}%`;
            $('.episode__num', node).textContent = String(ep.number);
            $('.episode__title', node).textContent = ep.title;
            $('.episode__dur', node).textContent = minutes(ep.runtime);
            $('.episode__desc', node).textContent = ep.overview;
            list.append(node);
        }
    };

    select.value = String(data.seasons[0].number);
    paint(data.seasons[0].number);
    select.addEventListener('change', () => paint(select.value));
}

function build(data) {
    const el = $('#tpl-detail').content.firstElementChild.cloneNode(true);
    $('.detail__dialog', el).setAttribute('aria-label', `${data.title} details`);

    const heroImg = $('.detail__hero img', el);
    setImageSrc(heroImg, data.backdrop_url);
    heroImg.alt = `${data.title} backdrop`;

    // A logo image stands in for the heading visually; the text stays for
    // assistive tech. With no logo, the text is simply shown.
    const logo = $('.detail__title img', el);
    const titleText = $('.detail__title-text', el);
    titleText.textContent = data.title;
    if (data.logo_url) {
        logo.alt = data.title;
        // /api/images needs Bearer auth, so the logo goes through the
        // authenticated blob fetch (setImageSrc), never a raw src. And it only
        // replaces the visible text once the image actually loads: a failed or
        // missing logo then falls back to the text title, not to nothing.
        logo.addEventListener('load', () => {
            logo.hidden = false;
            titleText.classList.add('u-visually-hidden');
        });
        setImageSrc(logo, data.logo_url);
    } else {
        logo.remove();
    }

    renderMeta(el, data);

    const resume = data.resume;
    $('.detail__play-label', el).textContent = resume?.label ?? 'PLAY';
    if (resume) {
        $('.detail__resume .progress__bar', el).style.width =
            `${progressPct(resume.position_sec, resume.duration_sec)}%`;
        $('.detail__resume-left', el).textContent =
            remaining(resume.position_sec, resume.duration_sec);
    } else {
        $('.detail__resume', el).remove();
    }

    $('.detail__tagline', el).textContent = data.tagline ?? '';
    $('.detail__overview', el).textContent = data.overview ?? '';

    renderEpisodes(el, data);

    const rows = $('.detail__rows', el);
    const people = castCrew(data.cast, data.crew);
    if (people.length) rows.append(row('Cast & Crew', people, personCard, 'row--people'));
    if (data.similar?.length) rows.append(row('More Like This', data.similar, posterCard));

    return el;
}

// castCrew builds the ordered "Cast & Crew" list from the separate cast and crew
// the API ships. Showing everyone is too much, so it's a small hierarchy capped
// at 12: directors (and TV creators) first, then top-billed actors, then at most
// one writer. Producers never appear (the scanner already keeps only
// Director/Writer/Creator crew, so there are none to drop).
//
// Writers are the lowest tier and only take space the cast didn't use, so they
// show up rarely: on short-cast titles that leave a slot free under the cap. On a
// full billing they simply don't fit, and an auteur who writes and directs is
// already deduped into the director entry. Everyone is deduped by person id, so a
// headliner who also has a writing credit stays where the cast placed them.
const MAX_PEOPLE = 12;
const MAX_WRITERS = 1;

function castCrew(cast = [], crew = []) {
    const directors = crew.filter((c) => c.role === 'Director' || c.role === 'Creator');
    const seen = new Set(directors.map((d) => d.id));

    // Actors fill the list right after the directors, up to the cap.
    const actors = [];
    for (const c of cast) {
        if (directors.length + actors.length >= MAX_PEOPLE || seen.has(c.id)) continue;
        seen.add(c.id);
        actors.push(c);
    }

    // Writers claim only what's left over -- never displacing a billed actor.
    const writers = [];
    for (const c of crew) {
        const used = directors.length + actors.length + writers.length;
        if (c.role !== 'Writer' || seen.has(c.id)
            || writers.length >= MAX_WRITERS || used >= MAX_PEOPLE) continue;
        seen.add(c.id);
        writers.push(c);
    }

    return [...directors, ...actors, ...writers].slice(0, MAX_PEOPLE);
}

export function createDetailModal(mount, { onSelect, onOpen, onClose } = {}) {
    let el = null;
    let lastFocused = null;

    function onKeydown(e) {
        if (!el) return;

        if (e.key === 'Escape') {
            close();
            return;
        }

        if (e.key !== 'Tab') return;
        const items = $$(FOCUSABLE, el).filter((n) => n.offsetParent !== null);
        if (!items.length) return;
        const first = items[0];
        const last = items[items.length - 1];
        if (e.shiftKey && document.activeElement === first) {
            e.preventDefault();
            last.focus();
        } else if (!e.shiftKey && document.activeElement === last) {
            e.preventDefault();
            first.focus();
        }
    }

    document.addEventListener('keydown', onKeydown);

    function show(data) {
        const restoreTo = el ? lastFocused : document.activeElement;
        if (el) el.remove();

        el = build(data);
        mount.append(el);
        lastFocused = restoreTo;

        el.addEventListener('click', (e) => {
            if (e.target === el || e.target.closest('[data-close]')) {
                close();
                return;
            }
            // Cards inside the modal swap its contents rather than stacking a
            // second dialog on top.
            const card = e.target.closest('.card[data-id]');
            if (card) {
                e.preventDefault();
                onSelect?.(card.dataset.kind, card.dataset.id);
            }
        });

        document.body.classList.add('is-modal-open');
        // Let the initial styles land so the open transition actually runs.
        requestAnimationFrame(() => el?.classList.add('is-open'));
        el.setAttribute('aria-hidden', 'false');
        $('[data-close]', el).focus();
        onOpen?.();
    }

    function close() {
        if (!el) return;
        el.remove();
        el = null;
        document.body.classList.remove('is-modal-open');
        if (typeof lastFocused?.focus === 'function') lastFocused.focus();
        lastFocused = null;
        onClose?.();
    }

    return { show, close, get isOpen() { return el !== null; } };
}
