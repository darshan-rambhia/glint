/* ── Table column sorting — survives htmx fragment swaps ──────────────────── */
(function () {
  'use strict';

  // Persists sort state across htmx swaps: tableId → { key, dir }
  const state = {};

  // Return the sort value for a cell: prefer data-sort-value, fall back to trimmed text.
  function cellValue(cell) {
    return cell.dataset.sortValue !== undefined
      ? cell.dataset.sortValue
      : cell.textContent.trim();
  }

  // Numeric-aware comparison; falls back to locale string compare.
  function compare(aCell, bCell, dir) {
    const a = cellValue(aCell);
    const b = cellValue(bCell);
    const an = Number(a), bn = Number(b);
    const cmp = (!isNaN(an) && !isNaN(bn))
      ? an - bn
      : a.localeCompare(b, undefined, { sensitivity: 'base' });
    return dir === 'asc' ? cmp : -cmp;
  }

  // Collect row groups from tbody. The disks table uses a trailing
  // .disk-detail-row that must stay paired with its parent after a sort.
  function rowGroups(tbody) {
    const rows = Array.from(tbody.rows);
    const groups = [];
    let i = 0;
    while (i < rows.length) {
      if (rows[i].classList.contains('disk-detail-row')) {
        if (groups.length) groups[groups.length - 1].push(rows[i]);
        i++;
      } else {
        groups.push([rows[i]]);
        i++;
      }
    }
    return groups;
  }

  function sortTable(table) {
    const s = state[table.id];
    if (!s) return;

    const headers = Array.from(table.querySelectorAll('thead th'));
    const colIdx = headers.findIndex(function (h) { return h.dataset.sortKey === s.key; });
    if (colIdx < 0) return;

    const tbody = table.tBodies[0];
    if (!tbody) return;

    const groups = rowGroups(tbody);
    groups.sort(function (a, b) {
      const ac = a[0].cells[colIdx];
      const bc = b[0].cells[colIdx];
      return (ac && bc) ? compare(ac, bc, s.dir) : 0;
    });
    groups.forEach(function (g) { g.forEach(function (r) { tbody.appendChild(r); }); });
  }

  function updateIndicators(table) {
    const s = state[table.id];
    table.querySelectorAll('thead th[data-sort-key]').forEach(function (th) {
      th.classList.toggle('sort-asc',  !!(s && th.dataset.sortKey === s.key && s.dir === 'asc'));
      th.classList.toggle('sort-desc', !!(s && th.dataset.sortKey === s.key && s.dir === 'desc'));
    });
  }

  function onClick(e) {
    const th = e.currentTarget;
    const table = th.closest('table[id]');
    if (!table) return;

    const key = th.dataset.sortKey;
    const cur = state[table.id];
    // First click on a column: ascending. Same column again: toggle.
    const dir = (cur && cur.key === key && cur.dir === 'asc') ? 'desc' : 'asc';
    state[table.id] = { key: key, dir: dir };

    sortTable(table);
    updateIndicators(table);
  }

  // Attach click handlers to all sortable headers in document (or a root node).
  function attach(root) {
    (root || document).querySelectorAll('table[id] thead th[data-sort-key]').forEach(function (th) {
      th.removeEventListener('click', onClick);
      th.addEventListener('click', onClick);
    });
  }

  // Re-apply any saved sort state after htmx swaps in fresh HTML.
  function reapply() {
    Object.keys(state).forEach(function (id) {
      const table = document.getElementById(id);
      if (table) { sortTable(table); updateIndicators(table); }
    });
  }

  document.addEventListener('DOMContentLoaded', function () { attach(); });
  document.addEventListener('htmx:afterSettle', function () { attach(); reapply(); });
}());
