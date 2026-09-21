function escapeAttribute(value) {
  return String(value)
    .replaceAll('&', '&amp;')
    .replaceAll('"', '&quot;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
}

function tileURL(baseURL, template, zoom, x, y, version) {
  const base = baseURL.endsWith('/') ? baseURL : `${baseURL}/`
  const path = `${base}${template}`
    .replace('{z}', String(zoom))
    .replace('{x}', String(x))
    .replace('{y}', String(y))
  return version ? `${path}?v=${encodeURIComponent(version)}` : path
}

function assetURL(baseURL, path, version) {
  const base = baseURL.endsWith('/') ? baseURL : `${baseURL}/`
  const url = `${base}${path}`
  return version ? `${url}?v=${encodeURIComponent(version)}` : url
}

export function buildRasterSVG(map, baseURL, version = '') {
  const raster = map?.raster
  if (!raster) return null
  const tilesPerAxis = 2 ** raster.zoom
  const size = raster.tileSize * tilesPerAxis
  const groups = raster.layers.map((layer) => {
    const images = []
    for (let y = 0; y < tilesPerAxis; y += 1) {
      for (let x = 0; x < tilesPerAxis; x += 1) {
        const href = tileURL(baseURL, layer.tilePath, raster.zoom, x, y, version)
        images.push(
          `<image href="${escapeAttribute(href)}" x="${x * raster.tileSize}" y="${y * raster.tileSize}" width="${raster.tileSize}" height="${raster.tileSize}"/>`,
        )
      }
    }
    return `<g id="${escapeAttribute(layer.floor)}">${images.join('')}</g>`
  })
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${size} ${size}" width="${size}" height="${size}">${groups.join('')}</svg>`
}

export function buildFallbackSVG(map, baseURL, version = '') {
  const width = Number(map?.fallbackImageWidth)
  const height = Number(map?.fallbackImageHeight)
  if (!map?.fallbackImage || !Number.isFinite(width) || !Number.isFinite(height) || width <= 0 || height <= 0) return null
  const href = assetURL(baseURL, map.fallbackImage, version)
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${width} ${height}" width="${width}" height="${height}"><image href="${escapeAttribute(href)}" x="0" y="0" width="${width}" height="${height}" preserveAspectRatio="xMidYMid meet"/></svg>`
}
