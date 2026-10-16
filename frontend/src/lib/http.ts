const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''

export function resolveApiUrl(urlOrPath: string) {
  const v = (urlOrPath ?? '').trim()
  if (!v) return ''
  if (/^https?:\/\//i.test(v)) return v
  // treat as path on API origin
  return API_BASE + v
}

async function parseJson(res: Response) {
  const text = await res.text()
  try {
    return text ? JSON.parse(text) : null
  } catch {
    return text
  }
}

export async function apiGet<T>(path: string, opts?: { token?: string }) {
  const res = await fetch(API_BASE + path, {
    method: 'GET',
    headers: {
      ...(opts?.token ? { Authorization: `Bearer ${opts.token}` } : {}),
    },
  })
  if (!res.ok) {
    const body = await parseJson(res)
    throw new Error(
      typeof body === 'string'
        ? body
        : (typeof body?.message === 'string' ? body.message : null) ?? body?.error ?? `Request failed (${res.status})`,
    )
  }
  return (await parseJson(res)) as T
}

export async function apiPost<T>(path: string, body: unknown, opts?: { token?: string }) {
  const res = await fetch(API_BASE + path, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      ...(opts?.token ? { Authorization: `Bearer ${opts.token}` } : {}),
    },
    body: JSON.stringify(body),
  })
  if (!res.ok) {
    const payload = await parseJson(res)
    throw new Error(
      typeof payload === 'string'
        ? payload
        : (typeof payload?.message === 'string' ? payload.message : null) ??
            payload?.error ??
            `Request failed (${res.status})`,
    )
  }
  return (await parseJson(res)) as T
}

export async function apiPatch<T>(path: string, body: unknown, opts?: { token?: string }) {
  const res = await fetch(API_BASE + path, {
    method: 'PATCH',
    headers: {
      'Content-Type': 'application/json',
      ...(opts?.token ? { Authorization: `Bearer ${opts.token}` } : {}),
    },
    body: JSON.stringify(body ?? {}),
  })
  if (!res.ok) {
    const parsed = await parseJson(res)
    throw new Error(
      typeof parsed === 'string'
        ? parsed
        : (typeof parsed?.message === 'string' ? parsed.message : null) ?? parsed?.error ?? `Request failed (${res.status})`,
    )
  }
  return (await parseJson(res)) as T
}

export async function apiPut<T>(path: string, body: unknown, opts?: { token?: string }) {
  const res = await fetch(API_BASE + path, {
    method: 'PUT',
    headers: {
      'Content-Type': 'application/json',
      ...(opts?.token ? { Authorization: `Bearer ${opts.token}` } : {}),
    },
    body: JSON.stringify(body),
  })
  if (!res.ok) {
    const payload = await parseJson(res)
    throw new Error(
      typeof payload === 'string'
        ? payload
        : (typeof payload?.message === 'string' ? payload.message : null) ??
            payload?.error ??
            `Request failed (${res.status})`,
    )
  }
  return (await parseJson(res)) as T
}

export async function apiDelete<T>(path: string, opts?: { token?: string }) {
  const res = await fetch(API_BASE + path, {
    method: 'DELETE',
    headers: {
      ...(opts?.token ? { Authorization: `Bearer ${opts.token}` } : {}),
    },
  })
  if (!res.ok) {
    const payload = await parseJson(res)
    throw new Error(
      typeof payload === 'string'
        ? payload
        : (typeof payload?.message === 'string' ? payload.message : null) ??
            payload?.error ??
            `Request failed (${res.status})`,
    )
  }
  return (await parseJson(res)) as T
}

export async function apiUpload<T>(
  path: string,
  formData: FormData,
  opts?: { token?: string },
) {
  const res = await fetch(API_BASE + path, {
    method: 'POST',
    headers: {
      ...(opts?.token ? { Authorization: `Bearer ${opts.token}` } : {}),
    },
    body: formData,
  })
  if (!res.ok) {
    const payload = await parseJson(res)
    throw new Error(
      typeof payload === 'string'
        ? payload
        : (typeof payload?.message === 'string' ? payload.message : null) ??
            payload?.error ??
            `Request failed (${res.status})`,
    )
  }
  return (await parseJson(res)) as T
}

