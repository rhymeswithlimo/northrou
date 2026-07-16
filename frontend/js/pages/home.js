// Home: featured hero, Continue Watching, and the recommendation rows.

import { $ } from '../lib/dom.js';
import { heroMeta } from '../lib/format.js';
import { mountNavAutoHide } from '../components/nav.js';
import { posterCard, continueCard, row } from '../components/card.js';
import { createDetailModal } from '../components/detail.js';
import { getHero, getContinueWatching, getHomeRows, getDetail } from '../data/library.js';

const rowsEl = $('#rows');
const heroEl = $('#hero');

const modal = createDetailModal($('#detail-root'), { onSelect: openDetail });

async function openDetail(kind, id) {
    const data = await getDetail(kind, id);
    if (data) modal.show(data);
}

function renderHero(item) {
    const el = $('#tpl-hero').content.firstElementChild.cloneNode(true);
    const img = $('img', el);
    img.src = item.backdrop_url;
    // The title sits in .hero__info right beside it, so the image adds nothing
    // for a screen reader.
    img.alt = '';
    $('.hero__title', el).textContent = item.title;
    $('.hero__meta', el).textContent = heroMeta(item);
    el.dataset.kind = item.kind;
    el.dataset.id = item.id;
    heroEl.replaceChildren(el);
}

async function render() {
    const [hero, continuing, rows] = await Promise.all([
        getHero(),
        getContinueWatching(),
        getHomeRows(),
    ]);

    if (hero) renderHero(hero);

    const nodes = [];
    if (continuing.length) {
        nodes.push(row('Continue Watching', continuing, continueCard, 'row--continue'));
    }
    for (const r of rows) {
        nodes.push(row(r.title, r.items, posterCard));
    }
    rowsEl.replaceChildren(...nodes);
}

// One delegated listener for every card and the hero, rather than one per node.
// Scoped to `main` so cards inside the modal don't reopen it from here; the
// modal handles its own.
document.addEventListener('click', (e) => {
    const target = e.target.closest('main .card[data-id], .hero__link[data-id]');
    if (!target) return;
    e.preventDefault();
    openDetail(target.dataset.kind, target.dataset.id);
});

mountNavAutoHide($('.nav'));
render();
