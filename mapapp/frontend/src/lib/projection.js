// World -> SVG coordinate projection (PLAN §4.1).
//
// 1. take the configured horizontal axes (default x,z)
// 2. normalize into [0,1]² using the map bounds (mirrorY: SVG y points down)
// 3. rotate around the center by the map's coordinateRotation
// 4. scale to the SVG viewBox
// 5. apply the calibration affine, if the map has been calibrated
//
// The affine absorbs systematic errors (axis order, mirror, crop), which is
// why the defaults above only need to be roughly right (PLAN §4.2).

export function horizontalOf(map, world) {
  const axes = map.projection.horizontalAxes || ['x', 'z']
  return [world[axes[0]], world[axes[1]]]
}

export function baseProject(map, world, svgSize) {
  const p = map.projection
  const b = map.bounds
  const [h, v] = horizontalOf(map, world)
  let u = (h - b.minX) / (b.maxX - b.minX)
  let w = (v - b.minZ) / (b.maxZ - b.minZ)
  if (p.mirrorX) u = 1 - u
  if (p.mirrorY) w = 1 - w
  const th = ((p.rotation || 0) * Math.PI) / 180
  const cos = Math.cos(th)
  const sin = Math.sin(th)
  const du = u - 0.5
  const dw = w - 0.5
  const u2 = 0.5 + du * cos - dw * sin
  const w2 = 0.5 + du * sin + dw * cos
  return [u2 * svgSize.w, w2 * svgSize.h]
}

export function applyAffine(af, x, y) {
  return [af[0] * x + af[1] * y + af[2], af[3] * x + af[4] * y + af[5]]
}

export function project(map, world, svgSize) {
  const [sx, sy] = baseProject(map, world, svgSize)
  const p = map.projection
  if (p.calibrated && p.affine) return applyAffine(p.affine, sx, sy)
  return [sx, sy]
}

// Unity quaternion -> camera forward vector, projected onto the horizontal plane
export function quatForward(q) {
  const fx = 2 * (q.x * q.z + q.w * q.y)
  const fz = 1 - 2 * (q.x * q.x + q.y * q.y)
  return [fx, fz]
}

// Heading angle in SVG screen coordinates by projecting the player position
// and a point slightly ahead — stays correct under rotation and affine.
export function headingAngle(map, world, quat, svgSize) {
  const [fx, fz] = quatForward(quat)
  if (Math.abs(fx) + Math.abs(fz) < 1e-6) return null
  const axes = map.projection.horizontalAxes || ['x', 'z']
  const d = { x: 0, y: 0, z: 0 }
  d[axes[0]] = fx
  d[axes[1]] = fz
  const eps = 5
  const p1 = project(map, world, svgSize)
  const p2 = project(map, { x: world.x + d.x * eps, y: world.y, z: world.z + d.z * eps }, svgSize)
  return Math.atan2(p2[1] - p1[1], p2[0] - p1[0])
}

// Least squares for A (n×3) -> b (n): solves (AᵀA)x = Aᵀb via Gaussian elimination.
function leastSquares3(A, b) {
  const M = [
    [0, 0, 0, 0],
    [0, 0, 0, 0],
    [0, 0, 0, 0],
  ]
  for (let i = 0; i < A.length; i++) {
    for (let r = 0; r < 3; r++) {
      for (let c = 0; c < 3; c++) M[r][c] += A[i][r] * A[i][c]
      M[r][3] += A[i][r] * b[i]
    }
  }
  for (let c = 0; c < 3; c++) {
    let piv = c
    for (let r = c + 1; r < 3; r++) if (Math.abs(M[r][c]) > Math.abs(M[piv][c])) piv = r
    ;[M[c], M[piv]] = [M[piv], M[c]]
    const d = M[c][c] || 1e-12
    for (let j = c; j < 4; j++) M[c][j] /= d
    for (let r = 0; r < 3; r++) {
      if (r === c) continue
      const f = M[r][c]
      for (let j = c; j < 4; j++) M[r][j] -= f * M[c][j]
    }
  }
  return [M[0][3], M[1][3], M[2][3]]
}

// Fit affine: baseProject(world) -> tapped SVG point. Returns { affine, rms }.
export function fitAffineFromPoints(map, points, svgSize) {
  const axes = map.projection.horizontalAxes || ['x', 'z']
  const rows = points.map((pt) => {
    const w = { x: 0, y: 0, z: 0 }
    w[axes[0]] = pt.w[0]
    w[axes[1]] = pt.w[1]
    return { src: baseProject(map, w, svgSize), dst: pt.s }
  })
  const A = rows.map((r) => [r.src[0], r.src[1], 1])
  const [a, b, c] = leastSquares3(A, rows.map((r) => r.dst[0]))
  const [d, e, f] = leastSquares3(A, rows.map((r) => r.dst[1]))
  const affine = [a, b, c, d, e, f]
  let se = 0
  for (const r of rows) {
    const [px, py] = applyAffine(affine, r.src[0], r.src[1])
    se += (px - r.dst[0]) ** 2 + (py - r.dst[1]) ** 2
  }
  return { affine, rms: Math.sqrt(se / rows.length), count: rows.length }
}
