// Card and row renderers, cloned from the <template>s in the page.

import { $ } from '../lib/dom.js';
import { remaining, progressPct, episodeCode, dotted } from '../lib/format.js';
import { setImageSrc } from '../api/images.js';

const tpl = (id) => $(`#${id}`).content.firstElementChild;

/** Poster-only card. */
export function posterCard(item) {
    const el = tpl('tpl-card').cloneNode(true);
    const img = $('img', el);
    setImageSrc(img, item.poster_url);
    img.alt = item.title;
    el.dataset.kind = item.kind;
    el.dataset.id = item.id;
    return el;
}

/** 16:9 card with a progress bar and "20m left" style meta. */
export function continueCard(item) {
    const el = tpl('tpl-card-continue').cloneNode(true);
    const img = $('img', el);
    setImageSrc(img, item.backdrop_url);
    img.alt = item.title;

    $('.progress__bar', el).style.width = `${progressPct(item.position_sec, item.duration_sec)}%`;
    $('.card__title', el).textContent = item.title;
    $('.card__meta', el).textContent = dotted(
        item.kind === 'episode' ? episodeCode(item.season, item.number) : '',
        remaining(item.position_sec, item.duration_sec),
    );

    el.dataset.kind = item.kind === 'episode' ? 'show' : item.kind;
    el.dataset.id = item.kind === 'episode' ? item.show_id : item.id;
    return el;
}

/** Circular avatar card for cast and crew. Not interactive. */
export function personCard(person) {
    const el = tpl('tpl-card-person').cloneNode(true);
    const img = $('img', el);
    setImageSrc(img, person.profile_url);
    img.alt = person.name;
    $('.card__title', el).textContent = person.name;
    $('.card__meta', el).textContent = person.role;
    return el;
}

/**
 * A titled row of cards.
 * @param {string} title
 * @param {Array} items
 * @param {(item) => Element} render
 * @param {string} [modifier] e.g. 'row--continue', 'row--people'
 */
export function row(title, items, render, modifier) {
    const el = tpl('tpl-row').cloneNode(true);
    if (modifier) el.classList.add(modifier);
    $('.row__title', el).textContent = title;
    const items_ = $('.row__items', el);
    for (const item of items) items_.append(render(item));
    return el;
}
