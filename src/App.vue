<script setup>
/**
 * 流星雨许愿 · 移动端
 * 建议宿主 index.html 使用以下 viewport（启用刘海屏安全区并禁止双指缩放）：
 * <meta name="viewport" content="width=device-width, initial-scale=1, maximum-scale=1, user-scalable=no, viewport-fit=cover" />
 *
 * 实现说明：
 * - 使用两层全屏 Canvas。流星层在下，负责“半透明黑色蒙层”残影；
 *   微星层在上，每帧重绘，避免残影蒙层把星空一起抹掉。
 * - 所有动画初始化在 onMounted，清理在 onUnmounted。
 */
import { computed, nextTick, onMounted, onUnmounted, reactive, ref } from 'vue'

/* ============================================================
 * 可调参数
 * ============================================================ */
const WISH_API_URL = import.meta.env.VITE_WISH_API_URL || '/api/wish'
const WISH_REQUEST_TIMEOUT = 10000      // 许愿接口超时时间
const METEOR_ANGLE = Math.PI / 4        // 流星统一方向：右上 -> 左下，45°
const TRAIL_FADE_AT_60FPS = 0.18        // 每帧黑色蒙层透明度（细长但不糊屏）
const STAR_MIN = 80                     // 微星数量下限
const STAR_MAX = 150                    // 微星数量上限
const BANNER_DURATION = 3000            // iOS 横幅停留时间
const WISH_NOTE_DURATION = 3600         // 愿望星星文字总展示时间
const WISH_NOTE_FADE_START = 2000       // 文字/星芒开始淡出的时间点
const AMBIENT_SPEED_MIN = 250           // 普通流星速度下限（减半：500 -> 250）
const AMBIENT_SPEED_MAX = 400           // 普通流星速度上限（减半：800 -> 400）
const WISH_SPEED_MIN = 180              // 许愿流星速度下限（减半：360 -> 180）
const WISH_SPEED_MAX = 260              // 许愿流星速度上限（减半：520 -> 260）
const METEOR_POOL_MIN = 20              // 同时存在的普通流星数量下限
const METEOR_POOL_MAX = 30              // 同时存在的普通流星数量上限
const METEOR_AREA_PER_ITEM = 17000      // 每多少屏幕像素维持一颗流星
const EDGE_FADE = 110                   // 淡出距离上限；窄屏会按宽度缩短
const TAU = Math.PI * 2

/* ============================================================
 * 响应式状态
 * ============================================================ */
const wisherName = ref('')              // 许愿人输入框
const wishText = ref('')                // 愿望输入框
const feedback = ref(null)              // 顶部横幅内容（成功 / 失败）
const isSubmitting = ref(false)         // 许愿接口请求中
const invalidField = ref('')            // 校验未通过的字段：wisher / wish / both
const stageRef = ref(null)
const starCanvas = ref(null)            // 微星层 Canvas
const meteorCanvas = ref(null)          // 流星层 Canvas
const wisherInputRef = ref(null)
const inputRef = ref(null)
const composerRef = ref(null)
const meteors = ref([])                 // 流星对象数组（环境流星 + 许愿流星）
const wishNote = ref(null)              // 绑定在星星上的愿望文字
const canSubmit = computed(
  () => wisherName.value.trim().length > 0 && wishText.value.trim().length > 0
)

/* ============================================================
 * 非响应式运行时变量（不参与渲染，避免每帧触发额外更新）
 * ============================================================ */
let starCtx = null
let meteorCtx = null
let stars = []
let width = 0
let height = 0
let dpr = 1
let rafId = 0
let lastTimestamp = 0
let bannerTimer = 0
let wishNoteTimer = 0
let meteorSequence = 0
let wishNoteSequence = 0
let feedbackSequence = 0
let isUnmounted = false
let prefersReducedMotion = false

const random = (min, max) => min + Math.random() * (max - min)

// 手机屏幕小但视口窄，保持 20+ 颗同时存在才能填满整屏
function getTargetMeteorCount() {
  return Math.round(
    Math.min(
      METEOR_POOL_MAX,
      Math.max(METEOR_POOL_MIN, (width * height) / METEOR_AREA_PER_ITEM)
    )
  )
}

/* ============================================================
 * Canvas 尺寸与上下文
 * ============================================================ */
function sizeCanvas() {
  const rect = stageRef.value?.getBoundingClientRect()
  width = Math.max(1, Math.round(rect?.width || window.innerWidth))
  height = Math.max(1, Math.round(rect?.height || window.innerHeight))
  // 移动端 DPR 通常为 3，限制在 2 可在清晰度与性能之间取得平衡
  dpr = Math.min(window.devicePixelRatio || 1, 2)

  const meteorEl = meteorCanvas.value
  const starEl = starCanvas.value
  if (!meteorEl || !starEl) return

  for (const canvas of [meteorEl, starEl]) {
    canvas.width = Math.round(width * dpr)
    canvas.height = Math.round(height * dpr)
    canvas.style.width = `${width}px`
    canvas.style.height = `${height}px`
  }

  meteorCtx = meteorEl.getContext('2d')
  starCtx = starEl.getContext('2d')
  meteorCtx.setTransform(dpr, 0, 0, dpr, 0, 0)
  starCtx.setTransform(dpr, 0, 0, dpr, 0, 0)

  // 流星层铺满纯黑，作为拖尾残影的“画布底”
  meteorCtx.fillStyle = '#000000'
  meteorCtx.fillRect(0, 0, width, height)
}

/* ============================================================
 * 微星：80~150 颗，1~2px，缓慢呼吸
 * ============================================================ */
function initStars() {
  const count = Math.floor(random(STAR_MIN, STAR_MAX + 1))
  stars = Array.from({ length: count }, () => {
    const base = random(0.26, 0.54)
    return {
      x: random(0, width),
      y: random(0, height),
      size: random(1, 2),
      minAlpha: Math.max(0.2, base - random(0.04, 0.1)),
      maxAlpha: Math.min(0.8, base + random(0.14, 0.26)),
      baseAlpha: base,
      phase: random(0, TAU),
      speed: random(0.28, 0.72) // 呼吸角速度，周期约 9~22 秒
    }
  })
}

function drawStars(now) {
  if (!starCtx) return
  starCtx.clearRect(0, 0, width, height)
  starCtx.fillStyle = '#ffffff'

  const t = now / 1000
  for (const star of stars) {
    const breath = prefersReducedMotion
      ? 0
      : 0.5 + 0.5 * Math.sin(t * star.speed + star.phase)
    const alpha = prefersReducedMotion
      ? star.baseAlpha
      : star.minAlpha + (star.maxAlpha - star.minAlpha) * breath

    starCtx.globalAlpha = alpha
    starCtx.beginPath()
    starCtx.arc(star.x, star.y, star.size / 2, 0, TAU)
    starCtx.fill()
  }
  starCtx.globalAlpha = 1
  drawWishStar(now)
}

// 愿望星：在选中的星星位置绘制更亮的星芒，并随文字一起淡出
function drawWishStar(now) {
  if (!starCtx || !wishNote.value) return

  const age = now - wishNote.value.startedAt
  const alpha =
    age <= WISH_NOTE_FADE_START
      ? 1
      : Math.max(
          0,
          1 -
            (age - WISH_NOTE_FADE_START) /
              (WISH_NOTE_DURATION - WISH_NOTE_FADE_START)
        )

  if (alpha <= 0) return

  const pulse = prefersReducedMotion ? 1 : 1 + Math.sin(now / 240) * 0.12

  starCtx.save()
  starCtx.globalCompositeOperation = 'lighter'
  starCtx.globalAlpha = alpha
  starCtx.shadowBlur = 18
  starCtx.shadowColor = 'rgba(205, 225, 255, 0.95)'
  starCtx.fillStyle = '#ffffff'
  starCtx.beginPath()
  starCtx.arc(wishNote.value.x, wishNote.value.y, 1.8 * pulse, 0, TAU)
  starCtx.fill()
  starCtx.restore()
}

/* ============================================================
 * 流星对象与绘制
 * ============================================================ */
function createMeteor(options) {
  return reactive({
    id: ++meteorSequence,
    x: options.x,
    y: options.y,
    vx: options.vx,
    vy: options.vy,
    tailLength: options.tailLength,
    lineWidth: options.lineWidth,
    headRadius: options.headRadius,
    baseOpacity: options.baseOpacity ?? 1,
    opacity: 0,
    age: options.age ?? 0,
    delay: options.delay ?? 0,
    isWish: Boolean(options.isWish)
  })
}

// 四象限随机取点；右下角占比最高，避免手机下半屏右侧空场
function randomSpawnPoint() {
  const roll = Math.random()

  // 30% 右下
  if (roll < 0.3) {
    return {
      x: random(width * 0.5, width * 0.98),
      y: random(height * 0.45, height * 0.84)
    }
  }

  // 25% 右上
  if (roll < 0.55) {
    return {
      x: random(width * 0.5, width * 0.98),
      y: random(0, height * 0.45)
    }
  }

  // 25% 左上
  if (roll < 0.8) {
    return {
      x: random(width * 0.02, width * 0.5),
      y: random(0, height * 0.45)
    }
  }

  // 20% 左下
  return {
    x: random(width * 0.02, width * 0.5),
    y: random(height * 0.45, height * 0.84)
  }
}

// 细长、亮度差异明显的拖尾，接近参考图而不是“激光雨”
function createMeteorProfile() {
  return {
    tailLength: random(90, 260),
    lineWidth: random(0.55, 1.15),
    headRadius: random(0.7, 1.35),
    baseOpacity: random(0.28, 0.82)
  }
}

// 创建一颗普通流星；age > 0 用于首屏预热，直接带出完整拖尾
function addAmbientMeteorAt(x, y, options = {}) {
  const speed = random(AMBIENT_SPEED_MIN, AMBIENT_SPEED_MAX)
  const profile = createMeteorProfile()

  meteors.value.push(
    createMeteor({
      x,
      y,
      vx: -Math.cos(METEOR_ANGLE) * speed,
      vy: Math.sin(METEOR_ANGLE) * speed,
      ...profile,
      delay: options.delay ?? 0,
      age: options.age ?? 0
    })
  )
}

const PREWARM_ZONES = [
  { x: [0.5, 0.98], y: [0.45, 0.84] }, // 右下
  { x: [0.5, 0.98], y: [0, 0.45] },    // 右上
  { x: [0.02, 0.5], y: [0, 0.45] },    // 左上
  { x: [0.02, 0.5], y: [0.45, 0.84] }  // 左下
]

// 首屏 / 重置时四象限预热，打开页面立刻铺满全屏
function prewarmMeteors(count) {
  for (let index = 0; index < count; index += 1) {
    const zone = PREWARM_ZONES[index % PREWARM_ZONES.length]
    addAmbientMeteorAt(
      random(zone.x[0] * width, zone.x[1] * width),
      random(zone.y[0] * height, zone.y[1] * height),
      { age: random(0.5, 2) }
    )
  }
}

// 重建普通流星池；许愿流星不参与重建，避免打断许愿动画
function rebuildAmbientPool() {
  for (let index = meteors.value.length - 1; index >= 0; index -= 1) {
    if (!meteors.value[index].isWish) {
      meteors.value.splice(index, 1)
    }
  }
  prewarmMeteors(getTargetMeteorCount())
}

// 普通流星飞出画布后原地复活到新的随机位置，维持全屏密度
function resetAmbientMeteor(meteor) {
  const spawn = randomSpawnPoint()
  const speed = random(AMBIENT_SPEED_MIN, AMBIENT_SPEED_MAX)
  const profile = createMeteorProfile()

  meteor.x = spawn.x
  meteor.y = spawn.y
  meteor.vx = -Math.cos(METEOR_ANGLE) * speed
  meteor.vy = Math.sin(METEOR_ANGLE) * speed
  meteor.tailLength = profile.tailLength
  meteor.lineWidth = profile.lineWidth
  meteor.headRadius = profile.headRadius
  meteor.baseOpacity = profile.baseOpacity
  meteor.opacity = 0
  meteor.age = 0
  meteor.delay = random(0, 0.14)
}

// 许愿流星不再携带文字，只作为一次额外的普通流星
function launchWishMeteor() {
  const speed = random(WISH_SPEED_MIN, WISH_SPEED_MAX)

  meteors.value.push(
    createMeteor({
      // 保持从右上角进入的既有许愿动效
      x: width + 28,
      y: height * random(0.2, 0.3),
      vx: -Math.cos(METEOR_ANGLE) * speed,
      vy: Math.sin(METEOR_ANGLE) * speed,
      ...createMeteorProfile(),
      isWish: true
    })
  )
}

// 从现有星空里挑一颗偏右上方的星星，作为愿望文字的落点
function pickWishStar() {
  const candidates = stars.filter(
    (star) =>
      star.x >= width * 0.52 &&
      star.x <= width * 0.68 &&
      star.y >= height * 0.24 &&
      star.y <= height * 0.4
  )

  if (candidates.length) {
    return candidates[Math.floor(Math.random() * candidates.length)]
  }

  const targetX = width * 0.6
  const targetY = height * 0.3
  let nearest = { x: targetX, y: targetY }
  let nearestDistance = Infinity

  for (const star of stars) {
    const dx = star.x - targetX
    const dy = star.y - targetY
    const distance = dx * dx + dy * dy
    if (distance < nearestDistance) {
      nearestDistance = distance
      nearest = star
    }
  }

  return nearest
}

// 文字固定钉在星星上，不再跟随流星；到时间后自动移除
function showWishNote(text) {
  const star = pickWishStar()

  wishNote.value = {
    id: ++wishNoteSequence,
    text,
    x: star.x,
    y: star.y,
    maxWidth: Math.round(Math.min(width * 0.6, 380)),
    startedAt: performance.now()
  }

  if (wishNoteTimer) window.clearTimeout(wishNoteTimer)
  wishNoteTimer = window.setTimeout(() => {
    wishNote.value = null
    wishNoteTimer = 0
  }, WISH_NOTE_DURATION)
}

function wishNoteStyle() {
  if (!wishNote.value) return {}
  return {
    left: `${wishNote.value.x}px`,
    top: `${wishNote.value.y - 14}px`,
    maxWidth: `${wishNote.value.maxWidth}px`
  }
}

function drawMeteor(meteor) {
  if (!meteorCtx || meteor.opacity <= 0.002) return

  const length = Math.hypot(meteor.vx, meteor.vy) || 1
  const dirX = meteor.vx / length
  const dirY = meteor.vy / length
  const tailX = meteor.x - dirX * meteor.tailLength
  const tailY = meteor.y - dirY * meteor.tailLength
  const alpha = meteor.opacity

  // 渐变光轨：尾端完全透明，越靠近头部越亮
  const trail = meteorCtx.createLinearGradient(tailX, tailY, meteor.x, meteor.y)
  trail.addColorStop(0, 'rgba(255, 255, 255, 0)')
  trail.addColorStop(0.35, `rgba(174, 201, 255, ${0.08 * alpha})`)
  trail.addColorStop(0.72, `rgba(212, 230, 255, ${0.38 * alpha})`)
  trail.addColorStop(0.92, `rgba(240, 247, 255, ${0.82 * alpha})`)
  trail.addColorStop(1, `rgba(255, 255, 255, ${alpha})`)

  meteorCtx.save()
  meteorCtx.globalCompositeOperation = 'lighter'
  meteorCtx.lineCap = 'round'
  meteorCtx.strokeStyle = trail

  // 外层柔光（更细，避免多颗流星把画面糊成雾）
  meteorCtx.globalAlpha = 0.08
  meteorCtx.lineWidth = meteor.lineWidth * 1.8
  meteorCtx.beginPath()
  meteorCtx.moveTo(tailX, tailY)
  meteorCtx.lineTo(meteor.x, meteor.y)
  meteorCtx.stroke()

  // 主光轨
  meteorCtx.globalAlpha = 1
  meteorCtx.lineWidth = meteor.lineWidth
  meteorCtx.beginPath()
  meteorCtx.moveTo(tailX, tailY)
  meteorCtx.lineTo(meteor.x, meteor.y)
  meteorCtx.stroke()

  // 头部光晕：许愿流星不额外放大
  const glowRadius = meteor.headRadius * 4.6
  const glow = meteorCtx.createRadialGradient(
    meteor.x,
    meteor.y,
    0,
    meteor.x,
    meteor.y,
    glowRadius
  )
  glow.addColorStop(0, `rgba(255, 255, 255, ${alpha})`)
  glow.addColorStop(0.18, `rgba(248, 251, 255, ${0.92 * alpha})`)
  glow.addColorStop(0.45, `rgba(188, 213, 255, ${0.24 * alpha})`)
  glow.addColorStop(1, 'rgba(140, 170, 255, 0)')

  meteorCtx.fillStyle = glow
  meteorCtx.beginPath()
  meteorCtx.arc(meteor.x, meteor.y, glowRadius, 0, TAU)
  meteorCtx.fill()

  // 高亮核心
  meteorCtx.fillStyle = `rgba(255, 255, 255, ${alpha})`
  meteorCtx.beginPath()
  meteorCtx.arc(meteor.x, meteor.y, meteor.headRadius * 0.58, 0, TAU)
  meteorCtx.fill()
  meteorCtx.restore()
}

function drawMeteorLayer(dt) {
  if (!meteorCtx) return

  // 帧率补偿后的半透明黑色蒙层：每帧叠加，形成细腻残影
  const fadeAlpha = 1 - Math.pow(1 - TRAIL_FADE_AT_60FPS, Math.max(dt, 0.001) * 60)
  meteorCtx.globalCompositeOperation = 'source-over'
  meteorCtx.fillStyle = `rgba(0, 0, 0, ${fadeAlpha})`
  meteorCtx.fillRect(0, 0, width, height)

  for (let index = meteors.value.length - 1; index >= 0; index -= 1) {
    const meteor = meteors.value[index]

    // 复活 / 预热时短暂延迟，避免所有流星同一帧整齐出现
    if (meteor.delay > 0) {
      meteor.delay -= dt
      continue
    }

    meteor.age += dt
    meteor.x += meteor.vx * dt
    meteor.y += meteor.vy * dt

    // 快速淡入，让画面中部的随机起点也能被清楚看到
    const fadeIn = Math.min(1, meteor.age / 0.18)

    // 贴近左/下边界时平滑淡出
    // 手机宽度较窄时缩短淡出距离，避免左下区域的流星过早被压暗
    const edgeFadeDistance = Math.min(EDGE_FADE, width * 0.2)
    let edgeFade = 1
    if (meteor.x < edgeFadeDistance) {
      edgeFade = Math.max(0, meteor.x / edgeFadeDistance)
    }
    if (meteor.y > height - edgeFadeDistance) {
      edgeFade = Math.min(
        edgeFade,
        Math.max(0, (height - meteor.y) / edgeFadeDistance)
      )
    }

    meteor.opacity = meteor.baseOpacity * fadeIn * edgeFade

    // 普通流星飞出画布后立即在随机位置复活，维持全屏流星数量
    // 许愿流星飞出后销毁
    if (meteor.x < -90 || meteor.y > height + 90) {
      if (meteor.isWish) {
        meteors.value.splice(index, 1)
      } else {
        resetAmbientMeteor(meteor)
      }
      continue
    }

    drawMeteor(meteor)
  }

  meteorCtx.globalCompositeOperation = 'source-over'
}

/* ============================================================
 * 动画主循环
 * ============================================================ */
function tick(now) {
  if (isUnmounted) return

  if (!lastTimestamp) lastTimestamp = now
  const dt = Math.min((now - lastTimestamp) / 1000, 0.05) // 防止切回标签页时位置跳变
  lastTimestamp = now

  drawMeteorLayer(dt)
  drawStars(now)

  rafId = requestAnimationFrame(tick)
}

/* ============================================================
 * 用户交互
 * ============================================================ */
function nudgeComposer() {
  const element = composerRef.value
  if (!element || prefersReducedMotion || typeof element.animate !== 'function') return

  element.animate(
    [
      { transform: 'translateX(0)' },
      { transform: 'translateX(-7px)' },
      { transform: 'translateX(6px)' },
      { transform: 'translateX(-3px)' },
      { transform: 'translateX(0)' }
    ],
    { duration: 360, easing: 'cubic-bezier(0.36, 0.07, 0.19, 0.97)' }
  )
}

function showFeedback(type, title, subtitle) {
  if (bannerTimer) window.clearTimeout(bannerTimer)
  feedback.value = { id: ++feedbackSequence, type, title, subtitle }
  bannerTimer = window.setTimeout(
    () => {
      feedback.value = null
      bannerTimer = 0
    },
    type === 'error' ? BANNER_DURATION + 800 : BANNER_DURATION
  )
}

function clearInvalidField() {
  invalidField.value = ''
}

function validateForm(wisher, text) {
  if (!wisher || !text) {
    if (!wisher && !text) {
      invalidField.value = 'both'
      showFeedback('error', '信息不完整', '请填写许愿人和愿望')
      nextTick(() => wisherInputRef.value?.focus())
    } else if (!wisher) {
      invalidField.value = 'wisher'
      showFeedback('error', '信息不完整', '请填写许愿人')
      nextTick(() => wisherInputRef.value?.focus())
    } else {
      invalidField.value = 'wish'
      showFeedback('error', '信息不完整', '请填写愿望')
      nextTick(() => inputRef.value?.focus())
    }
    return false
  }

  invalidField.value = ''
  return true
}

function vibrate() {
  if (typeof navigator.vibrate !== 'function') return
  try {
    navigator.vibrate(12)
  } catch {
    // 部分浏览器会拒绝震动权限，忽略即可
  }
}

async function sendWish() {
  if (isSubmitting.value) return

  const wisher = wisherName.value.trim()
  const text = wishText.value.trim()

  if (!validateForm(wisher, text)) {
    nudgeComposer()
    return
  }

  isSubmitting.value = true
  const controller = new AbortController()
  const timeoutId = window.setTimeout(() => controller.abort(), WISH_REQUEST_TIMEOUT)

  try {
    const response = await fetch(WISH_API_URL, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ wisher, wish: text }),
      signal: controller.signal
    })

    let payload = null
    try {
      payload = await response.json()
    } catch {
      payload = null
    }

    if (!response.ok || payload?.success !== true) {
      const fallbackMessage =
        response.status >= 500
          ? '许愿服务暂时不可用，请稍后重试'
          : `请求失败（HTTP ${response.status}）`
      showFeedback('error', '许愿失败', payload?.message || fallbackMessage)
      nudgeComposer()
      return
    }

    // 服务端保存成功后，再清空输入并播放许愿动效
    wisherName.value = ''
    wishText.value = ''
    invalidField.value = ''
    launchWishMeteor()
    showWishNote(text)
    showFeedback('success', '许愿成功', `${wisher}，你将得偿所愿`)
    vibrate()

    // 收起键盘，让用户完整看到流星划过
    inputRef.value?.blur()
  } catch (error) {
    showFeedback(
      'error',
      '许愿失败',
      error?.name === 'AbortError'
        ? '请求超时，请确认许愿服务正常后重试'
        : '网络异常，请确认许愿服务已启动后重试'
    )
    nudgeComposer()
  } finally {
    window.clearTimeout(timeoutId)
    isSubmitting.value = false
  }
}

function resetSky() {
  // 清空所有流星
  meteors.value.splice(0, meteors.value.length)
  if (meteorCtx) {
    meteorCtx.clearRect(0, 0, width, height)
    meteorCtx.fillStyle = '#000000'
    meteorCtx.fillRect(0, 0, width, height)
  }

  // 重新生成星空和普通流星池
  initStars()
  rebuildAmbientPool()

  wisherName.value = ''
  wishText.value = ''
  invalidField.value = ''
  feedback.value = null
  wishNote.value = null
  if (bannerTimer) {
    window.clearTimeout(bannerTimer)
    bannerTimer = 0
  }
  if (wishNoteTimer) {
    window.clearTimeout(wishNoteTimer)
    wishNoteTimer = 0
  }
}

/* ============================================================
 * 窗口尺寸 / 页面可见性
 * ============================================================ */
function handleResize() {
  // 尺寸变化后重建星空和普通流星池；正在飞行的许愿流星会保留
  sizeCanvas()
  initStars()
  rebuildAmbientPool()
}

function handleVisibilityChange() {
  if (document.hidden) {
    if (rafId) {
      cancelAnimationFrame(rafId)
      rafId = 0
    }
    return
  }

  lastTimestamp = 0
  if (!rafId && !isUnmounted) {
    rafId = requestAnimationFrame(tick)
  }
}

/* ============================================================
 * 生命周期：所有动画与监听都在这里初始化 / 清理
 * ============================================================ */
onMounted(() => {
  prefersReducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches

  sizeCanvas()
  initStars()
  // 首屏直接铺垫整屏流星，避免刚打开时大面积空场
  prewarmMeteors(getTargetMeteorCount())

  window.addEventListener('resize', handleResize, { passive: true })
  document.addEventListener('visibilitychange', handleVisibilityChange)

  rafId = requestAnimationFrame(tick)
})

onUnmounted(() => {
  isUnmounted = true

  if (rafId) cancelAnimationFrame(rafId)
  rafId = 0

  // 清理所有事件监听
  window.removeEventListener('resize', handleResize)
  document.removeEventListener('visibilitychange', handleVisibilityChange)

  // 清理所有定时器
  if (bannerTimer) window.clearTimeout(bannerTimer)
  if (wishNoteTimer) window.clearTimeout(wishNoteTimer)
  bannerTimer = 0
  wishNoteTimer = 0

  // 释放引用
  stars = []
  meteors.value = []
  wishNote.value = null
  starCtx = null
  meteorCtx = null
})
</script>

<template>
  <main ref="stageRef" class="wish-sky">
    <!--
      分层 Canvas：
      1. 流星层在下，使用黑色蒙层制造残影；
      2. 微星层在上，每帧重绘，保持星星清晰。
    -->
    <canvas ref="meteorCanvas" class="sky-canvas sky-canvas--meteors" aria-hidden="true"></canvas>
    <canvas ref="starCanvas" class="sky-canvas sky-canvas--stars" aria-hidden="true"></canvas>

    <div class="sky-vignette" aria-hidden="true"></div>

    <!-- 愿望文字：固定在选中的星星上方，不再跟随流星 -->
    <p
      v-if="wishNote"
      :key="wishNote.id"
      class="wish-note"
      :style="wishNoteStyle()"
      aria-hidden="true"
    >
      {{ wishNote.text }}
    </p>

    <!-- 顶部 iOS 风格通知横幅：成功与失败共用 -->
    <Transition name="banner">
      <div
        v-if="feedback"
        :key="feedback.id"
        class="banner-track"
        :role="feedback.type === 'error' ? 'alert' : 'status'"
        :aria-live="feedback.type === 'error' ? 'assertive' : 'polite'"
      >
        <div
          class="ios-banner"
          :class="{ 'ios-banner--error': feedback.type === 'error' }"
        >
          <div class="banner-icon" aria-hidden="true">
            {{ feedback.type === 'error' ? '!' : '✦' }}
          </div>
          <div class="banner-copy">
            <p class="banner-title">{{ feedback.title }}</p>
            <p class="banner-subtitle">{{ feedback.subtitle }}</p>
          </div>
          <span class="banner-now">now</span>
        </div>
      </div>
    </Transition>

    <!-- 左下角还原按钮 -->
    <button class="reset-button" type="button" aria-label="还原星空" @click="resetSky">
      <span class="reset-icon" aria-hidden="true">⟳</span>
      <span>还原</span>
    </button>

    <!-- 底部许愿输入胶囊：许愿人与愿望并列 -->
    <form class="composer" novalidate @submit.prevent="sendWish">
      <div
        ref="composerRef"
        class="composer-shell"
        :class="{ 'is-invalid': invalidField }"
      >
        <label class="composer-field composer-field--wisher">
          <span class="field-label" aria-hidden="true">许愿人</span>
          <input
            id="wisher-input"
            ref="wisherInputRef"
            v-model="wisherName"
            class="wish-input"
            type="text"
            inputmode="text"
            enterkeyhint="next"
            maxlength="20"
            placeholder="许愿人"
            aria-label="许愿人"
            aria-required="true"
            :aria-invalid="invalidField === 'wisher' || invalidField === 'both'"
            autocomplete="nickname"
            required
            @input="clearInvalidField"
          />
        </label>
        <span class="field-divider" aria-hidden="true"></span>
        <label class="composer-field composer-field--wish">
          <span class="field-label" aria-hidden="true">愿望</span>
          <input
            id="wish-input"
            ref="inputRef"
            v-model="wishText"
            class="wish-input"
            type="text"
            inputmode="text"
            enterkeyhint="send"
            maxlength="30"
            placeholder="对着流星许下愿望 "
            aria-label="愿望"
            aria-required="true"
            :aria-invalid="invalidField === 'wish' || invalidField === 'both'"
            autocomplete="off"
            autocapitalize="sentences"
            required
            @input="clearInvalidField"
          />
        </label>
        <button
          class="send-button"
          :class="{ 'is-active': canSubmit, 'is-submitting': isSubmitting }"
          type="submit"
          :disabled="isSubmitting"
          :aria-label="isSubmitting ? '正在许愿' : '发射愿望'"
          :aria-busy="isSubmitting"
        >
          <span aria-hidden="true">{{ isSubmitting ? '…' : '↑' }}</span>
        </button>
      </div>
    </form>
  </main>
</template>

<style scoped>
/* 锁住页面滚动与 iOS 橡皮筋回弹 */
:global(html),
:global(body) {
  margin: 0;
  padding: 0;
  width: 100%;
  height: 100%;
  overflow: hidden;
  background: #000000;
  overscroll-behavior: none;
  -webkit-text-size-adjust: 100%;
}

:global(#app) {
  height: 100%;
}

.wish-sky,
.wish-sky *,
.wish-sky *::before,
.wish-sky *::after {
  box-sizing: border-box;
}

.wish-sky {
  position: fixed;
  inset: 0;
  width: 100%;
  height: 100vh;
  height: 100dvh;
  overflow: hidden;
  color: #ffffff;
  background: #000000;
  font-family:
    -apple-system, BlinkMacSystemFont, "SF Pro Display", "SF Pro Text",
    "Helvetica Neue", "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei",
    sans-serif;
  touch-action: none;
  overscroll-behavior: none;
  -webkit-user-select: none;
  user-select: none;
  -webkit-tap-highlight-color: transparent;
}

/* ---------------- Canvas 与氛围层 ---------------- */
.sky-canvas {
  position: absolute;
  inset: 0;
  display: block;
  width: 100%;
  height: 100%;
  pointer-events: none;
}

.sky-canvas--meteors {
  z-index: 1;
  background: #000000;
}

.sky-canvas--stars {
  z-index: 2;
  animation: starfield-in 2.4s ease-out both;
}

@keyframes starfield-in {
  from {
    opacity: 0;
  }
  to {
    opacity: 1;
  }
}

.sky-vignette {
  position: absolute;
  inset: 0;
  z-index: 3;
  pointer-events: none;
  background:
    radial-gradient(
      110% 80% at 50% 40%,
      rgba(0, 0, 0, 0) 42%,
      rgba(0, 0, 0, 0.45) 100%
    ),
    linear-gradient(
      to bottom,
      rgba(0, 0, 0, 0.34) 0%,
      rgba(0, 0, 0, 0) 18%,
      rgba(0, 0, 0, 0.68) 68%,
      rgba(0, 0, 0, 0.58) 100%
    );
}

/* 手机端减轻左下角的暗角，确保穿过该区域的流星仍然可见 */
@media (max-width: 640px) {
  .sky-vignette {
    background:
      radial-gradient(
        110% 80% at 50% 40%,
        rgba(0, 0, 0, 0) 46%,
        rgba(0, 0, 0, 0.3) 100%
      ),
      linear-gradient(
        to bottom,
        rgba(0, 0, 0, 0.26) 0%,
        rgba(0, 0, 0, 0.16) 16%,
        rgba(0, 0, 0, 0.72) 72%,
        rgba(0, 0, 0, 0.42) 100%
      );
  }
}

/* ---------------- 许愿文字：绑定星星并淡出 ---------------- */
.wish-note {
  position: absolute;
  z-index: 5;
  width: max-content;
  margin: 0;
  padding: 2px 6px;
  color: #ffffff;
  font-size: clamp(13px, 3.8vw, 16px);
  font-weight: 500;
  line-height: 1.35;
  letter-spacing: 0.05em;
  text-align: center;
  overflow-wrap: anywhere;
  white-space: normal;
  text-shadow:
    0 0 4px rgba(255, 255, 255, 0.9),
    0 0 10px rgba(198, 219, 255, 0.38),
    0 0 22px rgba(135, 168, 255, 0.16);
  transform: translate(-50%, -100%);
  pointer-events: none;
  will-change: opacity, filter;
  animation: wish-note-life 3.6s ease-out forwards;
}

/* 淡入 -> 停留 -> 逐渐淡出；时长与 WISH_NOTE_DURATION 保持一致 */
@keyframes wish-note-life {
  0% {
    opacity: 0;
    filter: blur(3px);
  }
  10% {
    opacity: 1;
    filter: blur(0);
  }
  55% {
    opacity: 1;
  }
  100% {
    opacity: 0;
  }
}

/* ---------------- iOS 风格横幅 ---------------- */
.banner-track {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  z-index: 30;
  display: flex;
  justify-content: center;
  padding:
    calc(10px + env(safe-area-inset-top))
    max(12px, env(safe-area-inset-right))
    0
    max(12px, env(safe-area-inset-left));
  pointer-events: none;
}

.ios-banner {
  display: flex;
  align-items: center;
  width: min(100%, 430px);
  min-height: 64px;
  padding: 10px 14px 10px 12px;
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 20px;
  background: linear-gradient(
    155deg,
    rgba(112, 113, 118, 0.78),
    rgba(70, 71, 76, 0.84)
  );
  box-shadow:
    0 20px 50px rgba(0, 0, 0, 0.45),
    inset 0 1px 0 rgba(255, 255, 255, 0.16);
  -webkit-backdrop-filter: blur(20px) saturate(180%);
  backdrop-filter: blur(20px) saturate(180%);
}

.ios-banner--error {
  border-color: rgba(255, 174, 180, 0.22);
}

.ios-banner--error .banner-icon {
  border-color: rgba(255, 168, 176, 0.3);
  background:
    radial-gradient(
      circle at 35% 28%,
      rgba(255, 196, 201, 0.24),
      rgba(122, 44, 54, 0.18) 68%
    );
  color: #ffe3e6;
  text-shadow: 0 0 12px rgba(255, 156, 166, 0.55);
}

.banner-icon {
  display: grid;
  flex: 0 0 38px;
  width: 38px;
  height: 38px;
  margin-right: 11px;
  place-items: center;
  border: 1px solid rgba(255, 255, 255, 0.16);
  border-radius: 12px;
  background:
    radial-gradient(
      circle at 35% 28%,
      rgba(255, 255, 255, 0.28),
      rgba(255, 255, 255, 0.08) 68%
    );
  box-shadow:
    0 0 22px rgba(255, 255, 255, 0.08),
    inset 0 1px 0 rgba(255, 255, 255, 0.18);
  color: #ffffff;
  font-size: 22px;
  line-height: 1;
  text-shadow: 0 0 12px rgba(200, 220, 255, 0.9);
}

.banner-copy {
  min-width: 0;
  flex: 1;
}

.banner-title {
  margin: 0;
  color: #f7f8ff;
  font-size: 15px;
  font-weight: 600;
  line-height: 1.25;
  letter-spacing: 0.01em;
}

.banner-subtitle {
  margin: 2px 0 0;
  color: rgba(245, 247, 252, 0.66);
  font-size: 12px;
  font-weight: 400;
  line-height: 1.35;
  letter-spacing: 0.02em;
}

.banner-now {
  flex: 0 0 auto;
  align-self: flex-start;
  margin-top: 4px;
  color: rgba(245, 247, 252, 0.52);
  font-size: 12px;
  font-weight: 400;
  letter-spacing: 0.01em;
}

/* 顶部滑入 + 轻微回弹 */
.banner-enter-active {
  transition:
    transform 0.72s cubic-bezier(0.2, 1.35, 0.35, 1),
    opacity 0.32s ease-out;
}

.banner-leave-active {
  transition:
    transform 0.42s cubic-bezier(0.55, 0, 0.85, 0.4),
    opacity 0.28s ease-in;
}

.banner-enter-from,
.banner-leave-to {
  opacity: 0;
  transform: translateY(calc(-100% - 32px));
}

/* ---------------- 左下角还原按钮 ---------------- */
.reset-button {
  position: absolute;
  bottom: calc(88px + env(safe-area-inset-bottom));
  left: max(20px, env(safe-area-inset-left));
  z-index: 8;
  display: flex;
  align-items: center;
  gap: 6px;
  height: 34px;
  padding: 0 12px;
  border: 1px solid rgba(255, 255, 255, 0.09);
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.045);
  color: rgba(255, 255, 255, 0.5);
  font-family: inherit;
  font-size: 12px;
  letter-spacing: 0.08em;
  cursor: pointer;
  transition:
    color 0.25s ease,
    background 0.25s ease,
    border-color 0.25s ease,
    transform 0.18s ease;
}

.reset-button:active {
  transform: scale(0.95);
  border-color: rgba(255, 255, 255, 0.18);
  background: rgba(255, 255, 255, 0.1);
  color: #ffffff;
}

.reset-icon {
  font-size: 16px;
  line-height: 1;
}

/* ---------------- 底部许愿输入胶囊 ---------------- */
.composer {
  position: absolute;
  right: 0;
  bottom: calc(14px + env(safe-area-inset-bottom));
  left: 0;
  z-index: 10;
  display: flex;
  justify-content: center;
  padding-right: max(16px, env(safe-area-inset-right));
  padding-left: max(16px, env(safe-area-inset-left));
  pointer-events: none;
}

.composer-shell {
  display: flex;
  align-items: center;
  width: min(100%, 520px);
  height: 58px;
  padding: 7px 7px 7px 20px;
  border: 1px solid rgba(255, 255, 255, 0.13);
  border-radius: 999px;
  background: linear-gradient(
    158deg,
    rgba(37, 40, 49, 0.72),
    rgba(10, 11, 15, 0.9)
  );
  box-shadow:
    0 20px 50px rgba(0, 0, 0, 0.56),
    inset 0 1px 0 rgba(255, 255, 255, 0.08);
  pointer-events: auto;
  transition:
    border-color 0.3s ease,
    box-shadow 0.3s ease;
}

.composer-shell:focus-within {
  border-color: rgba(198, 214, 255, 0.36);
  box-shadow:
    0 20px 55px rgba(0, 0, 0, 0.62),
    0 0 0 1px rgba(160, 190, 255, 0.08),
    inset 0 1px 0 rgba(255, 255, 255, 0.12);
}

.composer-shell.is-invalid,
.composer-shell.is-invalid:focus-within {
  border-color: rgba(255, 164, 172, 0.48);
  box-shadow:
    0 20px 55px rgba(0, 0, 0, 0.62),
    0 0 0 1px rgba(255, 150, 160, 0.12),
    inset 0 1px 0 rgba(255, 255, 255, 0.1);
}

.wish-input {
  min-width: 0;
  height: 100%;
  flex: 1 1 auto;
  border: 0;
  outline: none;
  background: transparent;
  color: #f6f8ff;
  font-family: inherit;
  font-size: 16px;
  font-weight: 400;
  letter-spacing: 0.02em;
  line-height: 1;
  caret-color: #dfe8ff;
  -webkit-user-select: text;
  user-select: text;
  touch-action: manipulation;
}

.wish-input::placeholder {
  color: rgba(255, 255, 255, 0.34);
}

/* 桌面端保持单行并列；字段标签仅在手机双行布局中显示 */
.field-label {
  display: none;
}

.composer-field {
  display: flex;
  align-items: center;
  min-width: 0;
  height: 100%;
}

.composer-field--wisher {
  flex: 0 1 108px;
  padding-right: 14px;
}

.composer-field--wish {
  flex: 1 1 auto;
  min-width: 0;
}

.field-divider {
  flex: 0 0 1px;
  width: 1px;
  height: 20px;
  margin-right: 14px;
  background: rgba(255, 255, 255, 0.14);
}

.send-button {
  display: grid;
  flex: 0 0 44px;
  width: 44px;
  height: 44px;
  margin-left: 8px;
  padding: 0;
  place-items: center;
  border: 0;
  border-radius: 50%;
  background: rgba(255, 255, 255, 0.1);
  color: rgba(255, 255, 255, 0.46);
  font-family: inherit;
  font-size: 23px;
  font-weight: 500;
  line-height: 1;
  cursor: pointer;
  touch-action: manipulation;
  transition:
    color 0.25s ease,
    background 0.25s ease,
    box-shadow 0.25s ease,
    transform 0.16s ease;
}

.send-button.is-active {
  background: linear-gradient(145deg, #ffffff, #dbe4ff);
  color: #05060a;
  box-shadow:
    0 0 0 1px rgba(255, 255, 255, 0.34),
    0 0 26px rgba(180, 205, 255, 0.4);
}

.send-button:active {
  transform: scale(0.92);
}

.send-button:disabled {
  cursor: wait;
  transform: none;
}

.send-button.is-submitting {
  opacity: 0.72;
}

.reset-button:focus-visible,
.send-button:focus-visible {
  outline: 2px solid rgba(198, 216, 255, 0.78);
  outline-offset: 3px;
}

/* ---------------- 手机端：许愿人与愿望上下双行堆叠 ---------------- */
@media (max-width: 640px) {
  .composer {
    bottom: calc(10px + env(safe-area-inset-bottom));
  }

  .composer-shell {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 44px;
    grid-template-rows: 48px 48px;
    column-gap: 8px;
    height: auto;
    padding: 4px 7px 4px 16px;
    border-radius: 26px;
  }

  .composer-field {
    height: 100%;
  }

  .composer-field--wisher {
    grid-row: 1;
    grid-column: 1 / -1;
    padding-right: 0;
    border-bottom: 1px solid rgba(255, 255, 255, 0.09);
  }

  .composer-field--wish {
    grid-row: 2;
    grid-column: 1;
  }

  .field-divider {
    display: none;
  }

  .field-label {
    display: block;
    flex: 0 0 auto;
    margin-right: 12px;
    color: rgba(255, 255, 255, 0.36);
    font-size: 13px;
    line-height: 1;
    letter-spacing: 0.08em;
    transition: color 0.25s ease;
  }

  .composer-field:focus-within .field-label {
    color: rgba(221, 232, 255, 0.76);
  }

  /* 手机端已有固定字段标签，省略会重复的占位文字 */
  .wish-input::placeholder {
    color: transparent;
  }

  .send-button {
    grid-row: 2;
    grid-column: 2;
    align-self: center;
    margin-left: 0;
  }

  .reset-button {
    bottom: calc(130px + env(safe-area-inset-bottom));
  }
}

/* ---------------- 无障碍：减少动态效果 ---------------- */
@media (prefers-reduced-motion: reduce) {
  .sky-canvas--stars {
    animation: none;
  }

  .banner-enter-active,
  .banner-leave-active {
    transition-duration: 0.01ms;
  }
}
</style>