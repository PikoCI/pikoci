'use strict';

let _setToasts = null;

export function registerToastSetter(fn) { _setToasts = fn; }

export function showToast(msg, type) {
  if (!_setToasts) return;
  const id = Date.now();
  _setToasts(prev => [...prev, { id, msg, type, show: false }]);
  requestAnimationFrame(() => {
    if (!_setToasts) return;
    _setToasts(prev => prev.map(t => t.id === id ? { ...t, show: true } : t));
  });
  const duration = type === 'error' ? 8000 : 4000;
  setTimeout(() => dismissToast(id), duration);
}

export function dismissToast(id) {
  if (!_setToasts) return;
  _setToasts(prev => prev.map(t => t.id === id ? { ...t, show: false } : t));
  setTimeout(() => {
    if (!_setToasts) return;
    _setToasts(prev => prev.filter(t => t.id !== id));
  }, 300);
}
