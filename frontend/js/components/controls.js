// Builders for the form controls used across settings.

let uid = 0;

/**
 * Fill a <fieldset class="segmented"> with radio-backed options.
 * Radios rather than buttons so arrow keys, form semantics and screen-reader
 * group announcements all come for free.
 *
 * @param {HTMLFieldSetElement} field
 * @param {Array<{value: string, label: string}>} options
 * @param {string} value currently selected
 * @param {(value: string) => void} onChange
 */
export function segmented(field, options, value, onChange) {
    const name = `seg-${++uid}`;
    // Keep the <legend>; it names the group for assistive tech.
    for (const node of [...field.children]) {
        if (node.tagName !== 'LEGEND') node.remove();
    }

    for (const opt of options) {
        const id = `${name}-${opt.value}`;

        const input = document.createElement('input');
        input.type = 'radio';
        input.name = name;
        input.id = id;
        input.value = opt.value;
        input.checked = opt.value === value;
        input.addEventListener('change', () => onChange(opt.value));

        const label = document.createElement('label');
        label.className = 'segmented__option';
        label.htmlFor = id;
        label.textContent = opt.label;

        field.append(input, label);
    }
}

/** Reads/writes a .switch's checkbox. */
export function toggle(input, value, onChange) {
    input.checked = Boolean(value);
    input.addEventListener('change', () => onChange(input.checked));
}
