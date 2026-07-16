// The login page's rotating sphere of glyphs. Purely decorative: the canvas is
// aria-hidden and nothing here is load-bearing.
//
// It cycles formed -> unforming -> noise -> reforming, forever. The loop is
// paused whenever the canvas is offscreen, and never starts at all when the
// user has asked for reduced motion (a single static frame is drawn instead).

import { prefersReducedMotion } from '../lib/dom.js';

const SPHERE_SIZE = 0.45;
const SCATTER_SIZE = 0.95;
const EDGE_SOFTNESS = 30;
const GLYPH_MARGIN = 1;
const CHARS = ['n', 'o', 'r', 't', 'h', 'r', 'o', 'u'];
const POINT_COUNT = 260;

const HOLD_FORMED = 5200;
const UNFORM_DUR = 2200;
const HOLD_NOISE = 1800;
const REFORM_DUR = 2600;

const easeInOutCubic = (t) => (t < 0.5 ? 4 * t * t * t : 1 - Math.pow(-2 * t + 2, 3) / 2);

function randomNoiseTarget() {
    const z = Math.random() * 2 - 1;
    const theta = Math.random() * Math.PI * 2;
    const s = Math.sqrt(1 - z * z);
    const r = Math.cbrt(Math.random());
    return { x: s * Math.cos(theta) * r, y: s * Math.sin(theta) * r, z: z * r };
}

/** Fibonacci sphere, so the glyphs distribute evenly rather than clumping at the poles. */
function buildPoints(n) {
    const pts = [];
    const golden = Math.PI * (3 - Math.sqrt(5));
    for (let i = 0; i < n; i++) {
        const y = 1 - (i / (n - 1)) * 2;
        const r = Math.sqrt(1 - y * y);
        const theta = golden * i;
        pts.push({
            bx: Math.cos(theta) * r, by: y, bz: Math.sin(theta) * r,
            tx: 0, ty: 0, tz: 0,
            char: CHARS[Math.floor(Math.random() * CHARS.length)],
        });
    }
    return pts;
}

export function mountAsciiArt(canvas) {
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    const host = canvas.parentElement;

    let W, H, CX, CY, SAFE_RADIUS, RADIUS, SCATTER_RADIUS;
    const points = buildPoints(POINT_COUNT);

    let angleX = 0.3;
    let angleY = 0;
    let state = 'formed';
    let stateTime = 0;
    let lastTime = performance.now();
    let running = false;
    let frameId = null;

    function resize() {
        const rect = host.getBoundingClientRect();
        const dpr = window.devicePixelRatio || 1;
        const cssW = Math.max(rect.width, 1);
        const cssH = Math.max(rect.height, 1);

        canvas.width = Math.round(cssW * dpr);
        canvas.height = Math.round(cssH * dpr);
        ctx.setTransform(dpr, 0, 0, dpr, 0, 0);

        // Set once per resize rather than every frame.
        ctx.font = "11px 'Geist Mono', monospace";
        ctx.textAlign = 'center';
        ctx.textBaseline = 'middle';

        W = cssW;
        H = cssH;
        CX = W / 2;
        CY = H / 2;
        SAFE_RADIUS = Math.max(Math.min(CX, CY) - GLYPH_MARGIN, 1);
        RADIUS = SAFE_RADIUS * SPHERE_SIZE;
        SCATTER_RADIUS = SAFE_RADIUS * SCATTER_SIZE;
    }

    function advance(dt) {
        stateTime += dt;
        angleY += dt * 0.0003;
        angleX = 0.35 + Math.sin(performance.now() * 0.0001) * 0.12;

        if (state === 'formed' && stateTime > HOLD_FORMED) {
            state = 'unforming';
            stateTime = 0;
            for (const p of points) {
                const t = randomNoiseTarget();
                p.tx = t.x; p.ty = t.y; p.tz = t.z;
            }
        } else if (state === 'unforming' && stateTime > UNFORM_DUR) {
            state = 'noise';
            stateTime = 0;
        } else if (state === 'noise' && stateTime > HOLD_NOISE) {
            state = 'reforming';
            stateTime = 0;
            for (const p of points) {
                if (Math.random() < 0.3) p.char = CHARS[Math.floor(Math.random() * CHARS.length)];
            }
        } else if (state === 'reforming' && stateTime > REFORM_DUR) {
            state = 'formed';
            stateTime = 0;
        }

        if (state === 'formed') return 0;
        if (state === 'unforming') return easeInOutCubic(stateTime / UNFORM_DUR);
        if (state === 'noise') return 1;
        return 1 - easeInOutCubic(stateTime / REFORM_DUR);
    }

    function render(mix) {
        ctx.clearRect(0, 0, W, H);

        const cosX = Math.cos(angleX), sinX = Math.sin(angleX);
        const cosY = Math.cos(angleY), sinY = Math.sin(angleY);
        const scale = RADIUS + (SCATTER_RADIUS - RADIUS) * mix;
        const projected = [];

        for (const p of points) {
            const inv = 1 - mix;
            const lx = p.bx * inv + p.tx * mix;
            const ly = p.by * inv + p.ty * mix;
            const lz = p.bz * inv + p.tz * mix;

            const rx = lx * cosY - lz * sinY;
            let rz = lx * sinY + lz * cosY;
            const ry = ly * cosX - rz * sinX;
            rz = ly * sinX + rz * cosX;

            let px = CX + rx * scale;
            let py = CY + ry * scale;

            // Rubber-band anything past the safe radius back inside, so glyphs
            // compress at the edge instead of clipping.
            const dx = px - CX;
            const dy = py - CY;
            const dist = Math.sqrt(dx * dx + dy * dy);
            if (dist > SAFE_RADIUS) {
                const overflow = dist - SAFE_RADIUS;
                const compressed = SAFE_RADIUS + EDGE_SOFTNESS * (1 - Math.exp(-overflow / EDGE_SOFTNESS));
                const pull = compressed / dist;
                px = CX + dx * pull;
                py = CY + dy * pull;
            }

            projected.push({ px, py, depth: (rz + 1.4) / 2.8, char: p.char });
        }

        projected.sort((a, b) => a.depth - b.depth);

        for (const p of projected) {
            const alpha = Math.min(0.25 + p.depth * 0.9, 1);
            ctx.fillStyle = `rgba(255,255,255,${alpha.toFixed(3)})`;
            ctx.fillText(p.char, p.px, p.py);
        }
    }

    function step(now) {
        if (!running) return;
        const dt = Math.min(now - lastTime, 50); // cap so a backgrounded tab doesn't jump
        lastTime = now;
        render(advance(dt));
        frameId = requestAnimationFrame(step);
    }

    resize();

    if (prefersReducedMotion()) {
        render(0); // one static frame, no loop
        return;
    }

    let resizePending = false;
    const ro = new ResizeObserver(() => {
        if (resizePending) return;
        resizePending = true;
        requestAnimationFrame(() => {
            resize();
            resizePending = false;
        });
    });
    ro.observe(host);

    // Don't burn frames while scrolled out of view.
    const io = new IntersectionObserver((entries) => {
        if (entries[0].isIntersecting) {
            if (!running) {
                running = true;
                lastTime = performance.now();
                frameId = requestAnimationFrame(step);
            }
        } else {
            running = false;
            if (frameId) cancelAnimationFrame(frameId);
        }
    });
    io.observe(host);
}
