// Thin wrapper over the server's JSON API. Every failure becomes an
// ApiError carrying the server's plain-language message.

export class ApiError extends Error {
  constructor(status, code, message) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

async function request(method, path, body) {
  const init = { method, headers: {} };
  if (body !== undefined) {
    init.headers['Content-Type'] = 'application/json';
    init.body = JSON.stringify(body);
  }
  let res;
  try {
    res = await fetch(path, init);
  } catch {
    throw new ApiError(0, 'offline', "Can't reach the scanner app. Check that your phone is on the home Wi‑Fi.");
  }
  if (res.status === 204) return null;
  const data = await res.json().catch(() => null);
  if (!res.ok) {
    const err = data && data.error;
    throw new ApiError(res.status, err ? err.code : 'error', err ? err.message : `Something went wrong (${res.status}).`);
  }
  return data;
}

export const api = {
  status: () => request('GET', '/api/scanner/status'),
  options: () => request('GET', '/api/scan-options'),
  version: () => request('GET', '/api/version'),
  settings: () => request('GET', '/api/settings'),
  saveSettings: (s) => request('PUT', '/api/settings', s),

  scans: () => request('GET', '/api/scans'),
  scan: (id) => request('GET', `/api/scans/${id}`),
  startScan: (settings) => request('POST', '/api/scans', settings),
  updateScan: (id, patch) => request('PATCH', `/api/scans/${id}`, patch),
  deleteScan: (id) => request('DELETE', `/api/scans/${id}`),

  editPage: (id, page, patch) => request('PATCH', `/api/scans/${id}/pages/${page}`, patch),
  deletePage: (id, page) => request('DELETE', `/api/scans/${id}/pages/${page}`),
  reorder: (id, order) => request('PUT', `/api/scans/${id}/pages/order`, { order }),

  paperless: () => request('GET', '/api/paperless'),
  paperlessMetadata: () => request('GET', '/api/paperless/metadata'),
  send: (id, documents) => request('POST', `/api/scans/${id}/send`, { documents }),
};

export function pageImage(scanId, page, size, { uncropped = false, untoned = false } = {}) {
  const key = size === 'thumb' ? page.thumbKey : page.previewKey;
  const crop = uncropped ? '&crop=none' : '';
  const tone = untoned ? '&tone=none' : '';
  return `/api/scans/${scanId}/pages/${page.id}/image?size=${size}&v=${key}${crop}${tone}`;
}

export function pdfURL(scanId, pageIds) {
  const q = pageIds && pageIds.length ? `?pages=${pageIds.join(',')}` : '';
  return `/api/scans/${scanId}/pdf${q}`;
}
