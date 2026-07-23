// Profile picker.

import { $, reveal } from '../lib/dom.js';
import { statePanel, toast } from '../components/states.js';
import { listProfiles, createProfile, MAX_PROFILES } from '../data/profiles.js';
import { selectProfile } from '../data/account.js';
import { isSignedIn } from '../api/session.js';
import { requireServer } from '../api/connect.js';
import { watchingGreeting } from '../lib/greeting.js';

// Tile colours were hand-assigned per profile in the mockup. Profiles carry no
// colour of their own, so derive one deterministically from the id: the same
// profile always gets the same tile, and the first four match the original.
const TILES = ['#3c89e0', '#19ad31', '#d4412e', '#da3ce0', '#e0a13c', '#3cd6e0'];
const tileFor = (id) => TILES[(id - 1) % TILES.length];

const listEl = $('#profiles');
$('.profiles__title').textContent = watchingGreeting();

if (!(await requireServer())) throw new Error('no server');
if (!isSignedIn()) window.location.replace('connect.html');
else reveal();

function profileNode(profile) {
    const node = $('#tpl-profile').content.firstElementChild.cloneNode(true);
    const avatar = $('.profile__avatar', node);
    avatar.style.setProperty('--tile', tileFor(profile.id));
    $('.profile__monogram', node).textContent = profile.name.charAt(0).toUpperCase();
    $('.profile__name', node).textContent = profile.name;

    const link = $('.profile', node);
    link.dataset.id = profile.id;
    link.addEventListener('click', async (e) => {
        e.preventDefault();
        try {
            // Rescopes both tokens to this profile before the library loads,
            // so home rows are the right viewer's from the first request.
            await selectProfile(profile.id);
            window.location.assign('index.html');
        } catch {
            toast(`Could not switch to ${profile.name}.`, { variant: 'error' });
        }
    });
    return node;
}

function addNode() {
    const node = $('#tpl-profile-add').content.firstElementChild.cloneNode(true);
    $('.profile', node).addEventListener('click', async (e) => {
        e.preventDefault();
        const name = prompt('New profile name')?.trim();
        if (!name) return;
        try {
            await createProfile(name);
            await render();
            toast(`Added ${name}.`);
        } catch {
            toast('Could not add the profile.', { variant: 'error' });
        }
    });
    return node;
}

async function render() {
    let profiles;
    try {
        profiles = await listProfiles();
    } catch (err) {
        if (err.isAuth) {
            window.location.replace('connect.html');
            return;
        }
        listEl.replaceChildren(statePanel({
            variant: 'error',
            title: "Couldn't load profiles",
            body: 'The server did not respond. Check that it is running, then try again.',
            action: { label: 'Retry', onClick: render },
        }));
        return;
    }

    const nodes = profiles.map(profileNode);
    if (profiles.length < MAX_PROFILES) nodes.push(addNode());
    listEl.replaceChildren(...nodes);
}

render();
