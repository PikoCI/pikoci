'use strict';

export function toggleTheme() {
  var isDark = document.documentElement.getAttribute('data-theme') === 'dark';
  if (isDark) {
    document.documentElement.removeAttribute('data-theme');
    localStorage.setItem('piko-theme', 'light');
  } else {
    document.documentElement.setAttribute('data-theme', 'dark');
    localStorage.setItem('piko-theme', 'dark');
  }
  syncThemeSwitch();
}

export function syncThemeSwitch() {
  var t = document.getElementById('theme-toggle');
  if (t) {
    if (document.documentElement.getAttribute('data-theme') === 'dark') {
      t.classList.add('on');
    } else {
      t.classList.remove('on');
    }
  }
}

export function exportDatabase() {
  var jwt = window.app.session ? window.app.session.get('jwt') : '';
  if (!jwt) { window.app.apiNotice.set({error: 'Not authenticated'}); return; }
  fetch('/admin/export', {
    headers: { 'Authorization': 'Bearer ' + jwt }
  }).then(function(resp) {
    if (!resp.ok) {
      return resp.text().then(function(body) {
        throw new Error(body || resp.statusText);
      });
    }
    return resp.blob();
  }).then(function(blob) {
    var url = URL.createObjectURL(blob);
    var a = document.createElement('a');
    a.href = url;
    a.download = 'pikoci.db';
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  }).catch(function(err) {
    window.app.apiNotice.set({error: err.message});
  });
}

export var fetchInterval = 2000;

export function pikoTimeAgo(dateStr) {
  if (!dateStr || dateStr.startsWith('0001-01-01')) return 'never';
  var seconds = Math.floor((Date.now() - new Date(dateStr).getTime()) / 1000);
  if (seconds < 60) return 'just now';
  if (seconds < 3600) return Math.floor(seconds / 60) + 'm ago';
  if (seconds < 86400) return Math.floor(seconds / 3600) + 'h ago';
  return Math.floor(seconds / 86400) + 'd ago';
}

export var durationToString = function(duration) {
  if (duration === 0) {
    return "00:00:00";
  }
  var secConst = 1000*1000*1000;
  var minConst = secConst*60;
  var hourConst = minConst*60;

  var hours = Math.floor(duration / hourConst);
  var minutes = Math.floor((duration-(hours*hourConst))/minConst);
  var seconds = Math.floor((duration-(hours*hourConst)-(minutes*minConst))/secConst);
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

export var processLogs = function(text) {
  if (!text) return text;
  return text.split('\n').map(function(line) {
    if (line.indexOf('\r') !== -1) {
      var parts = line.split('\r');
      return parts[parts.length - 1];
    }
    return line;
  }).join('\n');
};

export var addSessionFunctions = function(data) {
  return _.extend(window.app.session.data(), data);
};

// Parse HCL error strings into diagnostics
export var parseHCLErrors = function(errorStr) {
  var results = [];
  // 1. Line/col based HCL errors
  var parts = errorStr.split(/((?:pipeline\.hcl|<stdin>):\d+,\d+-(?:\d+,)?\d+:\s*)/);
  for (var i = 1; i < parts.length; i += 2) {
    var header = parts[i];
    var body = (parts[i + 1] || '').trim();
    var hm = header.match(/(\d+),(\d+)-(?:(\d+),)?(\d+)/);
    if (!hm) continue;
    var msg = body.replace(/[;\s]+$/, '').trim();
    if (!msg) continue;
    results.push({
      line: parseInt(hm[1], 10),
      colStart: parseInt(hm[2], 10),
      colEnd: hm[3] ? parseInt(hm[4], 10) : parseInt(hm[4], 10),
      message: msg
    });
  }
  // 2. Source resolution errors
  var srcRe = /failed to resolve source for (\w+)\s+"([^"]+)":\s*(.+?)(?=failed to resolve source for|$)/g;
  var sm;
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
export var blockTypes = [
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

// Set up window.app and expose globals for HTML templates
window.app = {};
window.toggleTheme = toggleTheme;
window.exportDatabase = exportDatabase;
window.pikoTimeAgo = pikoTimeAgo;
window.processLogs = processLogs;
window.addSessionFunctions = addSessionFunctions;
