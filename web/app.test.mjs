// Run: node web/app.test.mjs
import assert from 'node:assert';
import { readFileSync } from 'node:fs';

// app.js touches document at load; stub just enough to import the helpers.
globalThis.document = { body: { addEventListener() {} } };
const src = readFileSync(new URL('./static/app.js', import.meta.url), 'utf8');
const { applyFormat } = await import('data:text/javascript,' +
    encodeURIComponent(src + '\nexport { applyFormat };'));

// Fake textarea: setRangeText + selection only.
const ta = (value, start, end) => ({
    value, selectionStart: start, selectionEnd: end,
    focus() {},
    dispatchEvent() {},
    setRangeText(text, s, e) {
        this.value = this.value.slice(0, s) + text + this.value.slice(e);
    },
    setSelectionRange(s, e) { this.selectionStart = s; this.selectionEnd = e; },
});

const t = ta('', 0, 0);
applyFormat(t, 'bold');
assert.equal(t.value, '**bold text**');
assert.equal(t.selectionStart, 2, 'caret inside bold wrapper');

const t2 = ta('hello world', 6, 11);
applyFormat(t2, 'h1');
assert.equal(t2.value, 'hello \n# world', 'heading prefix inserted on its own line');
assert.equal(t2.selectionStart, 9, 'caret after "# "');

const t3 = ta('', 0, 0);
applyFormat(t3, 'ol');
assert.equal(t3.value, '1. List item');

console.log('ok');
