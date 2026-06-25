'use strict';

import { useState, useEffect, useRef } from 'preact/hooks';
import { route } from 'preact-router';
import { isLoggedIn, session, isTeamAdmin, hasTeamRole } from './state.js';

export function useRequireAuth({ adminOnly = false, requiredRole = null, teamCanonical = null, redirectTo = null } = {}) {
  useEffect(() => {
    if (!isLoggedIn.value) {
      route('/login', true);
      return;
    }
    if (session.value.user?.must_change_password && window.location.pathname !== '/profile') {
      route('/profile', true);
      return;
    }
    const denied = requiredRole
      ? !hasTeamRole(teamCanonical, requiredRole)
      : adminOnly && !isTeamAdmin(teamCanonical);
    if (denied) {
      if (redirectTo) {
        route(redirectTo, true);
      } else if (teamCanonical) {
        route('/teams/' + teamCanonical + '/pipelines', true);
      } else {
        route('/', true);
      }
    }
  }, []);
  return isLoggedIn.value;
}

/**
 * usePolling — runs a callback on an interval, pausing when the tab is hidden.
 * Calls fn() immediately on mount, then every `interval` ms.
 * Stops when the tab becomes hidden, resumes (with immediate call) when visible again.
 * Cleans up on unmount.
 *
 * @param {Function} fn — async or sync callback to poll
 * @param {number} interval — milliseconds between polls
 * @param {boolean} [enabled=true] — set to false to pause polling
 */
export function usePolling(fn, interval, enabled = true) {
  const fnRef = useRef(fn);
  fnRef.current = fn;

  useEffect(() => {
    if (!enabled) return;

    let id = null;

    const start = () => {
      if (id !== null) return;
      try { fnRef.current(); } catch {}
      id = setInterval(() => {
        try { fnRef.current(); } catch {}
      }, interval);
    };

    const stop = () => {
      if (id !== null) {
        clearInterval(id);
        id = null;
      }
    };

    const onVisibility = () => {
      if (document.hidden) {
        stop();
      } else {
        start();
      }
    };

    document.addEventListener('visibilitychange', onVisibility);
    if (!document.hidden) {
      start();
    }

    return () => {
      stop();
      document.removeEventListener('visibilitychange', onVisibility);
    };
  }, [interval, enabled]);
}

export function useLoading() {
  const [loading, setLoading] = useState(false);

  const withLoading = async (fn) => {
    setLoading(true);
    try {
      return await fn();
    } finally {
      setLoading(false);
    }
  };

  return [loading, withLoading];
}
