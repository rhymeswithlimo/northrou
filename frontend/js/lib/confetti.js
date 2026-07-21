// Minimal canvas confetti burst. No deps, no config object soup: one export,
// fires once, cleans itself up after the last piece falls off screen.

import { prefersReducedMotion } from './dom.js';

const COLORS = ['#9b3ffd', '#14b8ff', '#5cff1a', '#ff4d73', '#f5ff2e'];
const PIECE_COUNT = 200;
const GRAVITY = 0.18;
const DRAG = 0.98; // per-frame velocity decay, gives a terminal fall speed
const MAX_DELAY = 210; // frames over which pieces trickle out, staggered (~1.8s longer at 60fps)
const STRAY_COUNT = 8; // a few last pieces that fall well after the rest
const STRAY_TAIL = 150; // frames past MAX_DELAY the strays are spread across

export function confetti() {
    if (prefersReducedMotion()) return;

    const canvas = document.createElement('canvas');
    canvas.style.cssText =
        'position:fixed;inset:0;width:100vw;height:100vh;pointer-events:none;z-index:9999;';
    canvas.width = window.innerWidth;
    canvas.height = window.innerHeight;
    document.body.appendChild(canvas);
    const ctx = canvas.getContext('2d');

    const makePiece = (delay) => ({
        x: Math.random() * canvas.width,
        y: -20 - Math.random() * 40,
        w: 5 + Math.random() * 8,
        h: 3 + Math.random() * 6,
        shape: Math.random() < 0.5 ? 'rect' : 'ellipse',
        color: COLORS[(Math.random() * COLORS.length) | 0],
        vx: -2.5 + Math.random() * 5,
        vy: Math.random() * 2,
        // sideways sway, independent of drift, so paths aren't parallel lines
        sway: Math.random() * Math.PI * 2,
        swaySpeed: 0.03 + Math.random() * 0.07,
        swayAmp: 0.4 + Math.random() * 1.2,
        // 2D rotation with a little random torque so spin isn't perfectly linear
        angle: Math.random() * Math.PI * 2,
        spinSpeed: -0.2 + Math.random() * 0.4,
        // separate tilt axis: fakes a card tumbling end-over-end in 3D by
        // squashing its drawn height, the classic paper-confetti flutter
        tilt: Math.random() * Math.PI * 2,
        tiltSpeed: 0.08 + Math.random() * 0.18,
        delay,
    });

    const pieces = [
        // biased toward starting sooner, with a long tail of stragglers,
        // instead of an evenly-spread uniform trickle
        ...Array.from({ length: PIECE_COUNT }, () =>
            makePiece(Math.pow(Math.random(), 1.3) * MAX_DELAY),
        ),
        // a handful of loose pieces well after the main trickle, so the tail
        // end doesn't cut off cleanly
        ...Array.from({ length: STRAY_COUNT }, () =>
            makePiece(MAX_DELAY + Math.random() * STRAY_TAIL),
        ),
    ];

    let elapsed = 0;
    let frame;
    function tick() {
        elapsed++;
        ctx.clearRect(0, 0, canvas.width, canvas.height);

        let allDone = true;
        for (const p of pieces) {
            if (elapsed < p.delay) {
                allDone = false;
                continue;
            }

            p.vx *= DRAG;
            p.vy = p.vy * DRAG + GRAVITY;
            p.sway += p.swaySpeed;
            p.spinSpeed += (Math.random() - 0.5) * 0.02; // light random torque
            p.spinSpeed *= 0.995;
            p.angle += p.spinSpeed;
            p.tilt += p.tiltSpeed;

            p.x += p.vx + Math.sin(p.sway) * p.swayAmp;
            p.y += p.vy;

            const flip = Math.cos(p.tilt);

            ctx.save();
            ctx.translate(p.x, p.y);
            ctx.rotate(p.angle);
            ctx.scale(1, flip);
            ctx.fillStyle = p.color;
            if (p.shape === 'ellipse') {
                ctx.beginPath();
                ctx.ellipse(0, 0, p.w / 2, p.h / 2, 0, 0, Math.PI * 2);
                ctx.fill();
            } else {
                ctx.fillRect(-p.w / 2, -p.h / 2, p.w, p.h);
            }
            ctx.restore();

            if (p.y < canvas.height + 20) allDone = false;
        }

        if (allDone) {
            canvas.remove();
            return;
        }
        frame = requestAnimationFrame(tick);
    }
    frame = requestAnimationFrame(tick);

    // Stop and clean up if the page navigates away mid-fall.
    window.addEventListener(
        'pagehide',
        () => {
            cancelAnimationFrame(frame);
            canvas.remove();
        },
        { once: true },
    );
}
