// Profile picker.

import { $ } from '../lib/dom.js';
import { listProfiles, MAX_PROFILES } from '../data/profiles.js';

// Tile colours were hand-assigned per profile in the mockup. Profiles carry no
// colour of their own, so derive one deterministically from the id: the same
// profile always gets the same tile, and the first four match the original.
const TILES = ['#3c89e0', '#19ad31', '#d4412e', '#da3ce0', '#e0a13c', '#3cd6e0'];
const tileFor = (id) => TILES[(id - 1) % TILES.length];

const listEl = $('#profiles');

function profileNode(profile) {
    const node = $('#tpl-profile').content.firstElementChild.cloneNode(true);
    const avatar = $('.profile__avatar', node);
    avatar.style.setProperty('--tile', tileFor(profile.id));
    $('.profile__monogram', node).textContent = profile.name.charAt(0).toUpperCase();
    $('.profile__name', node).textContent = profile.name;
    $('.profile', node).dataset.id = profile.id;
    return node;
}

async function render() {
    const profiles = await listProfiles();
    const nodes = profiles.map(profileNode);

    if (profiles.length < MAX_PROFILES) {
        nodes.push($('#tpl-profile-add').content.firstElementChild.cloneNode(true));
    }

    listEl.replaceChildren(...nodes);
}

render();
