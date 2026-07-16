// Hide the nav bar when scrolling down, reveal it when scrolling up.

const REVEAL_ZONE = 360;     // px from top where the nav always shows
const DELTA_THRESHOLD = 10;  // ignore jitter: toolbar collapse, overscroll bounce

export function mountNavAutoHide(nav) {
    if (!nav) return;

    // iOS/Android rubber-banding can push scrollY negative or past the max.
    function clampedY() {
        const maxY = Math.max(0, document.documentElement.scrollHeight - window.innerHeight);
        return Math.min(Math.max(window.scrollY, 0), maxY);
    }

    let lastY = clampedY();
    let ticking = false;

    function update() {
        const y = clampedY();
        const delta = y - lastY;

        if (y <= REVEAL_ZONE) {
            nav.classList.remove('nav--hidden');
            lastY = y;
        } else if (Math.abs(delta) >= DELTA_THRESHOLD) {
            // Only act once the move is big enough to be a real scroll, not the
            // browser chrome resizing the viewport.
            nav.classList.toggle('nav--hidden', delta > 0);
            lastY = y;
        }

        ticking = false;
    }

    window.addEventListener('scroll', () => {
        if (ticking) return;
        ticking = true;
        window.requestAnimationFrame(update);
    }, { passive: true });

    // A mobile address bar showing/hiding fires resize, not a real scroll
    // intent. Resync so it isn't misread as a big delta on the next event.
    let resizeTimer;
    window.addEventListener('resize', () => {
        clearTimeout(resizeTimer);
        resizeTimer = setTimeout(() => { lastY = clampedY(); }, 100);
    }, { passive: true });
}
