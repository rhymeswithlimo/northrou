// DEV FIXTURES - NOT SHIPPED CONTENT.
//
// This file exists only so the UI can be built and reviewed before the backend
// endpoints land. It is the sample data that used to be hardcoded into
// index.html; it lives here so the markup is templates rather than placeholder.
//
// Shapes mirror the real API (docs/api.md) exactly, so `js/data/library.js`
// swaps this module for `fetch` at integration with no change to the renderers:
//   home rows  -> GET /api/home            {key, title, items[]}
//   detail     -> GET /api/movies|shows/{id}
//   continue   -> GET /api/continue-watching
//
// DELETE THIS FILE once the API calls are wired.

const img = (p) => `https://image.tmdb.org/t/p/original${p}`;

export const hero = {
    kind: 'movie',
    id: 1,
    title: 'AVATAR: FIRE AND ASH',
    year: 2025,
    genres: ['Sci-Fi'],
    runtime: 195,
    backdrop_url: img('/q3qmJaaTQtWfqBHVytGrfisLduQ.jpg'),
};

export const continueWatching = [
    {
        kind: 'episode', id: 101, show_id: 10, title: 'SILO',
        season: 3, number: 2, position_sec: 2280, duration_sec: 3600,
        backdrop_url: img('/n5FPNMJ0eRoiQrKGfUQQRAZeaxg.jpg'),
    },
    {
        kind: 'movie', id: 20, title: 'PULP FICTION',
        position_sec: 6000, duration_sec: 9240,
        backdrop_url: img('/cOGysF4SXauEKz18mFGmkZW6O2T.jpg'),
    },
    {
        kind: 'movie', id: 30, title: 'THE LORD OF THE RINGS: THE FELLOWSHIP OF THE RING',
        position_sec: 1450, duration_sec: 8040,
        backdrop_url: img('/x2RS3uTcsJJ9IfjNPcgDmukoEcQ.jpg'),
    },
];

export const homeRows = [
    {
        key: 'you-might-like', title: 'You Might Like',
        items: [
            { kind: 'movie', id: 41, title: 'Marty Supreme', poster_url: img('/lYWEXbQgRTR4ZQleSXAgRbxAjvq.jpg') },
            { kind: 'show', id: 42, title: 'The Boys', poster_url: img('/in1R2dDc421JxsoRWaIIAqVI2KE.jpg') },
            { kind: 'movie', id: 43, title: 'Blade Runner 2049', poster_url: img('/gajva2L0rPYkEWjzgFlBXCAVBE5.jpg') },
            { kind: 'movie', id: 44, title: 'Avatar: Fire and Ash', poster_url: img('/bRBeSHfGHwkEpImlhxPmOcUsaeg.jpg') },
            { kind: 'show', id: 45, title: 'Severance', poster_url: img('/pPHpeI2X1qEd1CS1SeyrdhZ4qnT.jpg') },
        ],
    },
];

const siloEpisodes = [
    {
        id: 1001, season: 1, number: 1, title: 'Freedom Day', runtime: 60,
        overview: "Sheriff Becker's plans for the future are thrown off course after his wife meets a hacker with information about the silo.",
        still_url: 'https://i.ibb.co/35BSZ7QK/vlcsnap-2026-07-14-02h13m09s671.png',
        position_sec: 3600, duration_sec: 3600,
    },
    {
        id: 1002, season: 1, number: 2, title: "Holston's Pick", runtime: 47,
        overview: "Juliette, an engineer, pieces together what might have led to a co-worker's mysterious death.",
        still_url: 'https://i.ibb.co/9HpfLN9w/vlcsnap-2026-07-14-02h13m38s207.png',
        position_sec: 2820, duration_sec: 2820,
    },
    {
        id: 1003, season: 1, number: 3, title: 'Machines', runtime: 62,
        overview: 'In her search for a new sheriff, Mayor Ruth travels down to the deep levels to meet Juliette.',
        still_url: 'https://i.ibb.co/Z6XxLLb7/vlcsnap-2026-07-14-02h14m02s297.png',
        position_sec: 2418, duration_sec: 3720,
    },
];

const details = {
    'show:10': {
        kind: 'show', id: 10, title: 'Silo', year: 2023,
        rating: 8.1, certification: 'TV-MA', genres: ['Sci-Fi', 'Drama'],
        tagline: 'The truth will surface.',
        overview: 'In a ruined and toxic future, thousands live in a giant silo deep underground. After its sheriff breaks a cardinal rule and residents die mysteriously, engineer Juliette starts to uncover shocking secrets and the truth about the silo.',
        backdrop_url: img('/n5FPNMJ0eRoiQrKGfUQQRAZeaxg.jpg'),
        logo_url: img('/orJ5TfInOu5CzgFIDs4avqEg0wu.png'),
        resume: { label: 'RESUME S03:E02', position_sec: 2280, duration_sec: 3600 },
        seasons: [
            { number: 1, episodes: siloEpisodes },
            { number: 2, episodes: [] },
            { number: 3, episodes: [] },
        ],
        cast: [
            { name: 'Rebecca Ferguson', role: 'Juliette Nichols', profile_url: img('/lJloTOheuQSirSLXNA3JHsrMNfH.jpg') },
            { name: 'Tim Robbins', role: 'Bernard Holland', profile_url: img('/djLVFETFTvPyVUdrd7aLVykobof.jpg') },
            { name: 'Common', role: 'Robert Sims', profile_url: img('/eggJAnJxpn8wYraX7ea5svETB54.jpg') },
            { name: 'Harriet Walter', role: 'Martha Walker', profile_url: img('/vH8JrqdHaoFeGos44XeKTNuQMKE.jpg') },
            { name: 'Rashida Jones', role: 'Allison Becker', profile_url: img('/p7ObPxJzggeJ5Xzltls2SHuFn9o.jpg') },
        ],
        similar: [
            { kind: 'show', id: 51, title: 'Severance', poster_url: img('/mYLOqiStMxDK3fYZFirgrMt8z5d.jpg') },
            { kind: 'show', id: 52, title: 'Dark', poster_url: img('/apbrbWs8M9lyOpJYU5WXrpFbk1Z.jpg') },
            { kind: 'show', id: 53, title: 'Foundation', poster_url: img('/tg9I5pOY4M9CKj8U0cxVBTsm5eh.jpg') },
            { kind: 'show', id: 54, title: 'The Expanse', poster_url: img('/5vQlVWkIMPhZ88OWchJsgwGEK9.jpg') },
        ],
    },
    'movie:20': {
        kind: 'movie', id: 20, title: 'Pulp Fiction', year: 1994,
        rating: 8.5, certification: 'R', runtime: 154, genres: ['Crime', 'Thriller'],
        tagline: "Just because you are a character doesn't mean you have character.",
        overview: "Three loosely linked Los Angeles crime stories collide over a handful of days: two philosophizing hitmen running an errand for their boss, a fading prizefighter who takes the wrong payoff, and a mob wife's night out that goes sideways. Tarantino scrambles the timeline, letting diner stick-ups, a briefcase nobody explains, and a lot of very casual conversation braid into one story about luck, loyalty, and second chances.",
        backdrop_url: img('/suaEOtk1N1sgg2MTM7oZd2cfVp3.jpg'),
        logo_url: img('/kpuNKsIzVbK3LDVo4iOJDAY0y7d.png'),
        resume: { label: 'RESUME', position_sec: 2587, duration_sec: 9240 },
        cast: [
            { name: 'Quentin Tarantino', role: 'Director', profile_url: img('/1gjcpAa99FAOWGnrUvHEXXsRs7o.jpg') },
            { name: 'John Travolta', role: 'Vincent Vega', profile_url: img('/zyDLuyohFiON7QliYyP8hnxu2eX.jpg') },
            { name: 'Samuel L. Jackson', role: 'Jules Winnfield', profile_url: img('/nHa0el66nAf50XJXZwTbIIqUJwQ.jpg') },
            { name: 'Uma Thurman', role: 'Mia Wallace', profile_url: img('/hlYG0MC6im0MHNq1xixxVilfwyR.jpg') },
            { name: 'Bruce Willis', role: 'Butch Coolidge', profile_url: img('/mEVoLBcShTV8mYb4mo0aFlzWfnV.jpg') },
        ],
        similar: [
            { kind: 'movie', id: 61, title: 'Reservoir Dogs', poster_url: img('/xi8Iu6qyTfyZVDVy60raIOYJJmk.jpg') },
            { kind: 'movie', id: 62, title: 'Jackie Brown', poster_url: img('/rOUx7qg4KmEh1juEDwqzbDSL1Nr.jpg') },
            { kind: 'movie', id: 63, title: 'Kill Bill: Vol. 1', poster_url: img('/v7TaX8kXMXs5yFFGR41guUDNcnB.jpg') },
            { kind: 'movie', id: 64, title: 'Inglourious Basterds', poster_url: img('/aupnPtagH9JVBuMrGEanf4iqXEQ.jpg') },
            { kind: 'movie', id: 65, title: 'Django Unchained', poster_url: img('/7oWY8VDWW7thTzWh3OKYRkWUlD5.jpg') },
            { kind: 'movie', id: 66, title: 'Goodfellas', poster_url: img('/9OkCLM73MIU2CrKZbqiT8Ln1wY2.jpg') },
        ],
    },
};

export const detailFor = (kind, id) => details[`${kind}:${id}`] ?? null;
