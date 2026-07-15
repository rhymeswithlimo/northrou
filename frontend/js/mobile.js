// Universally disable :hover styling on touch devices.
// Runs after stylesheets load, walks every rule, deletes anything
// containing ":hover". No per-selector overrides needed.
(function () {
    const isTouch = window.matchMedia('(hover: none)').matches
        || ('ontouchstart' in window)
        || navigator.maxTouchPoints > 0;

    if (!isTouch) return;

    function stripHoverRules() {
        for (const sheet of document.styleSheets) {
            let rules;
            try {
                rules = sheet.cssRules;
            } catch (e) {
                // Cross-origin stylesheet (e.g. a CDN font/CSS link); can't
                // read its rules due to browser security. Skip it.
                continue;
            }
            if (!rules) continue;

            for (let i = rules.length - 1; i >= 0; i--) {
                const rule = rules[i];
                if (rule.selectorText && rule.selectorText.indexOf(':hover') !== -1) {
                    sheet.deleteRule(i);
                }
            }
        }
    }

    // Stylesheets loaded via <link> may not be parsed yet at the moment
    // this script runs, depending on where it's placed. Wait for full
    // page load to guarantee every sheet is ready, then strip.
    if (document.readyState === 'complete') {
        stripHoverRules();
    } else {
        window.addEventListener('load', stripHoverRules);
    }
})();