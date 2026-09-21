import { useEffect, useRef, useState, useCallback } from 'react'

// Server state via GET /api/state + live updates over SSE /api/events.
export function useServerState() {
  const [state, setState] = useState(null)
  const [connected, setConnected] = useState(false)

  useEffect(() => {
    let dead = false
    fetch('/api/state')
      .then((r) => r.json())
      .then((s) => !dead && setState(s))
      .catch(() => {})

    const es = new EventSource('/api/events')
    es.onopen = () => setConnected(true)
    es.onerror = () => setConnected(false)
    es.onmessage = (e) => {
      try {
        const msg = JSON.parse(e.data)
        if (msg.type === 'state') setState(msg.state)
      } catch {
        /* ignore malformed frames */
      }
    }
    return () => {
      dead = true
      es.close()
    }
  }, [])

  return { state, connected }
}

export async function postJSON(url, body) {
	return sendJSON('POST', url, body)
}

export async function putJSON(url, body) {
	return sendJSON('PUT', url, body)
}

async function sendJSON(method, url, body) {
  try {
    const resp = await fetch(url, {
	  method,
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })
    return { ok: resp.ok, data: await resp.json().catch(() => null) }
  } catch (err) {
    return { ok: false, data: null, error: err.message }
  }
}

export function useCatalog(mode, mapId, catalogVersion) {
  const [data, setData] = useState({ meta: null, tasks: [], features: [], coverage: null, progress: null })
  const [loading, setLoading] = useState(true)

  const refresh = useCallback(() => {
    let alive = true
    setLoading(true)
    const query = new URLSearchParams({ mode })
    const mapQuery = new URLSearchParams({ mode })
    if (mapId) mapQuery.set('mapId', mapId)
    Promise.all([
      fetch('/api/catalog/meta').then((r) => r.json()),
      fetch(`/api/tasks?${query}`).then((r) => r.json()),
      mapId
        ? fetch(`/api/maps/${mapId}/features?${mapQuery}`).then((r) => r.json())
        : Promise.resolve({ features: [], coverage: null }),
      fetch(`/api/progress?${query}`).then((r) => r.json()),
    ])
      .then(([meta, tasks, mapFeatures, progress]) => {
        if (!alive) return
        setData({
          meta,
          tasks: tasks.tasks || [],
          features: mapFeatures.features || [],
          coverage: mapFeatures.coverage || null,
          progress,
        })
      })
      .catch(() => {})
      .finally(() => alive && setLoading(false))
    return () => {
      alive = false
    }
  }, [mode, mapId, catalogVersion])

  useEffect(() => refresh(), [refresh])
  return { ...data, loading, refresh }
}

// Quest list is static per build — fetch once.
let questsCache = null
export function useQuests() {
  const [quests, setQuests] = useState(questsCache || [])
  useEffect(() => {
    if (questsCache) return
    fetch('/api/quests')
      .then((r) => r.json())
      .then((j) => {
        questsCache = j.quests || []
        setQuests(questsCache)
      })
      .catch(() => {})
  }, [])
  return quests
}

// Fetches and caches map SVG text by URL.
const svgCache = {}
export function useSvgText(url) {
  const [text, setText] = useState(svgCache[url] || null)
  const [error, setError] = useState(null)
  useEffect(() => {
    let dead = false
    setError(null)
    if (!url || !(url.startsWith('http') || url.startsWith('/'))) {
      setText(null)
      return
    }
    if (svgCache[url]) {
      setText(svgCache[url])
      return
    }
    setText(null)
    fetch(url)
      .then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`)
        return r.text()
      })
      .then((t) => {
        if (!/<svg(?:\s|>)/i.test(t) || !/viewBox\s*=/i.test(t)) throw new Error('SVG 格式无效')
        svgCache[url] = t
        if (!dead) setText(t)
      })
      .catch((e) => !dead && setError(e.message))
    return () => {
      dead = true
    }
  }, [url])
  return { text, error }
}
