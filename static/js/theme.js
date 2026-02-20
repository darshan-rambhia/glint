// theme.js — three-way theme switcher for Glint (light / dark / auto)
//
// The IIFE at the top runs synchronously while the browser is still parsing
// <head>, before <body> is painted. This applies the saved preference
// immediately and prevents a flash of the wrong theme.

(function () {
  var t = localStorage.getItem('glint-theme');
  if (t === 'light' || t === 'dark') {
    document.documentElement.setAttribute('data-theme', t);
  }
})();

// After the DOM is ready, wire up the switcher buttons.
document.addEventListener('DOMContentLoaded', function () {
  var switcher = document.getElementById('theme-switcher');
  if (!switcher) return;

  function applyTheme(t) {
    if (t === 'auto') {
      document.documentElement.removeAttribute('data-theme');
      localStorage.removeItem('glint-theme');
    } else {
      document.documentElement.setAttribute('data-theme', t);
      localStorage.setItem('glint-theme', t);
    }
    switcher.setAttribute('data-active', t);
  }

  // Reflect the saved preference (or 'auto') in the active button.
  switcher.setAttribute('data-active', localStorage.getItem('glint-theme') || 'auto');

  switcher.querySelectorAll('[data-theme-set]').forEach(function (btn) {
    btn.addEventListener('click', function () {
      applyTheme(this.getAttribute('data-theme-set'));
    });
  });
});
