'use strict';

export const fetchInterval = 2000;

export const durationToString = (duration) => {
  if (duration === 0) {
    return "00:00:00";
  }
  const secConst = 1000*1000*1000;
  const minConst = secConst*60;
  const hourConst = minConst*60;

  let hours = Math.floor(duration / hourConst);
  let minutes = Math.floor((duration-(hours*hourConst))/minConst);
  let seconds = Math.floor((duration-(hours*hourConst)-(minutes*minConst))/secConst);
  hours = hours || "00";
  minutes = minutes || "00";
  seconds = seconds || "00";
  hours +="";
  minutes +="";
  seconds +="";
  if (hours.length === 1) {
    hours = "0"+hours;
  }
  if (minutes.length === 1) {
    minutes = "0"+minutes;
  }
  if (seconds.length === 1) {
    seconds = "0"+seconds;
  }
  return hours+ ":" + minutes + ":" + seconds;
};

export const processLogs = (text) => {
  if (!text) return text;
  return text.split('\n').map((line) => {
    if (line.indexOf('\r') !== -1) {
      const parts = line.split('\r');
      return parts[parts.length - 1];
    }
    return line;
  }).join('\n');
};

export const pikoTimeAgo = (dateStr) => {
  if (!dateStr || dateStr.startsWith('0001-01-01')) return 'never';
  const seconds = Math.floor((Date.now() - new Date(dateStr).getTime()) / 1000);
  if (seconds < 60) return 'just now';
  if (seconds < 3600) return Math.floor(seconds / 60) + 'm ago';
  if (seconds < 86400) return Math.floor(seconds / 3600) + 'h ago';
  return Math.floor(seconds / 86400) + 'd ago';
};

// Parse HCL error strings into diagnostics
export const parseHCLErrors = (errorStr) => {
  const results = [];
  // 1. Line/col based HCL errors
  const parts = errorStr.split(/((?:pipeline\.hcl|<stdin>):\d+,\d+-(?:\d+,)?\d+:\s*)/);
  for (let i = 1; i < parts.length; i += 2) {
    const header = parts[i];
    const body = (parts[i + 1] || '').trim();
    const hm = header.match(/(\d+),(\d+)-(?:(\d+),)?(\d+)/);
    if (!hm) continue;
    const msg = body.replace(/[;\s]+$/, '').trim();
    if (!msg) continue;
    results.push({
      line: parseInt(hm[1], 10),
      colStart: parseInt(hm[2], 10),
      colEnd: hm[3] ? parseInt(hm[4], 10) : parseInt(hm[4], 10),
      message: msg
    });
  }
  // 2. Source resolution errors
  const srcRe = /failed to resolve source for (\w+)\s+"([^"]+)":\s*(.+?)(?=failed to resolve source for|$)/g;
  let sm;
  while ((sm = srcRe.exec(errorStr)) !== null) {
    results.push({
      blockType: sm[1],
      blockName: sm[2],
      attribute: 'source',
      message: sm[3].replace(/[;\s]+$/, '').trim()
    });
  }
  // Fallback: if no structured errors found, show full error on line 1
  if (results.length === 0 && errorStr && errorStr.trim()) {
    results.push({line: 1, colStart: 1, colEnd: 2, message: errorStr.trim()});
  }
  return results;
};

// Block type metadata for the blocks panel
export const blockTypes = [
  {type: 'resource_type', label: 'Resource Types', icon: 'rt', letter: 'R'},
  {type: 'resource',      label: 'Resources',      icon: 'rs', letter: 'R'},
  {type: 'job',           label: 'Jobs',            icon: 'jb', letter: 'J'},
  {type: 'secret_type',   label: 'Secret Types',    icon: 'st', letter: 'S'},
  {type: 'runner_type',   label: 'Runner Types',    icon: 'rn', letter: 'R'},
  {type: 'service_type',       label: 'Service Types',       icon: 'sv', letter: 'S'},
  {type: 'notification_type', label: 'Notification Types', icon: 'nt', letter: 'N'},
  {type: 'notification',      label: 'Notifications',      icon: 'no', letter: 'N'},
  {type: 'variable',          label: 'Variables',           icon: 'vr', letter: 'V'},
];

export const toggleTheme = () => {
  const isDark = document.documentElement.getAttribute('data-theme') === 'dark';
  if (isDark) {
    document.documentElement.removeAttribute('data-theme');
    localStorage.setItem('piko-theme', 'light');
  } else {
    document.documentElement.setAttribute('data-theme', 'dark');
    localStorage.setItem('piko-theme', 'dark');
  }
  syncThemeSwitch();
};

export const syncThemeSwitch = () => {
  const t = document.getElementById('theme-toggle');
  if (t) {
    if (document.documentElement.getAttribute('data-theme') === 'dark') {
      t.classList.add('on');
    } else {
      t.classList.remove('on');
    }
  }
};

export const exportDatabase = (jwt) => {
  if (!jwt) return;
  fetch('/admin/export', {
    headers: { 'Authorization': 'Bearer ' + jwt }
  }).then((resp) => {
    if (!resp.ok) {
      return resp.text().then((body) => {
        throw new Error(body || resp.statusText);
      });
    }
    return resp.blob();
  }).then((blob) => {
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'pikoci.db';
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  });
};

// Sort builds by build number (major.minor) descending.
// Build numbers are strings like "1.2"; sort by major first, then minor.
export const sortBuilds = (builds) => {
  return [...builds].sort((a, b) => {
    const [aMajor, aMinor] = (a.number || '0').split('.').map(Number);
    const [bMajor, bMinor] = (b.number || '0').split('.').map(Number);
    if (bMajor !== aMajor) return bMajor - aMajor;
    return (bMinor || 0) - (aMinor || 0);
  });
};

// Select the active build: prefer the newest started or pending build, else return the first.
export const selectActiveBuild = (builds, requestedID) => {
  if (!builds || builds.length === 0) return null;
  if (requestedID) {
    const found = builds.find((b) => String(b.id) === String(requestedID));
    if (found) return found;
  }
  const active = builds.find((b) => b.status === 'started' || b.status === 'pending');
  return active || builds[0];
};

// Extract a human-readable ref string from a version metadata map.
export const versionRef = (v) => {
  if (!v) return '';
  if (typeof v === 'string') return v;
  if (v.ref) return v.ref;
  if (v.digest) return v.digest;
  if (v.tag) return v.tag;
  if (typeof v.version === 'string') return v.version;
  for (const key in v) {
    if (v.hasOwnProperty(key)) {
      return key + ': ' + v[key];
    }
  }
  return '';
};
