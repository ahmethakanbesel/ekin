/* Ekin composition runtime.
 *
 * A composition is a page whose every frame is a pure function of time.
 * It calls Ekin.composition({...}); the renderer then calls
 * window.ekin.renderAt(t) for each frame and screenshots the page.
 * Never use requestAnimationFrame loops, CSS animations, timers or
 * Date.now(): they are not driven by t and will not render.
 */
(function () {
  "use strict";

  const clamp = (x, a = 0, b = 1) => Math.min(b, Math.max(a, x));
  const lerp = (a, b, p) => a + (b - a) * p;
  const progress = (t, start, end) => (end <= start ? (t >= end ? 1 : 0) : clamp((t - start) / (end - start)));

  const ease = {
    linear: (x) => x,
    inQuad: (x) => x * x,
    outQuad: (x) => 1 - (1 - x) * (1 - x),
    inOutQuad: (x) => (x < 0.5 ? 2 * x * x : 1 - Math.pow(-2 * x + 2, 2) / 2),
    outCubic: (x) => 1 - Math.pow(1 - x, 3),
    inOutCubic: (x) => (x < 0.5 ? 4 * x * x * x : 1 - Math.pow(-2 * x + 2, 3) / 2),
    outExpo: (x) => (x === 1 ? 1 : 1 - Math.pow(2, -10 * x)),
    outBack: (x) => { const c1 = 1.70158, c3 = c1 + 1; return 1 + c3 * Math.pow(x - 1, 3) + c1 * Math.pow(x - 1, 2); },
    outElastic: (x) => (x === 0 || x === 1 ? x : Math.pow(2, -10 * x) * Math.sin((x * 10 - 0.75) * ((2 * Math.PI) / 3)) + 1),
  };

  /** tween(t, start, duration, easing) → eased 0..1 */
  const tween = (t, start, duration, fn = ease.inOutCubic) => fn(progress(t, start, start + duration));

  /** Deterministic PRNG (mulberry32): same seed, same sequence. */
  function random(seed = 1) {
    let a = seed >>> 0;
    return function () {
      a = (a + 0x6d2b79f5) >>> 0;
      let r = Math.imul(a ^ (a >>> 15), 1 | a);
      r = (r + Math.imul(r ^ (r >>> 7), 61 | r)) ^ r;
      return ((r ^ (r >>> 14)) >>> 0) / 4294967296;
    };
  }

  /** Loads narration/timings.json written by `ekin tool tts`. */
  async function timings(path = "narration/timings.json") {
    const res = await fetch(path);
    if (!res.ok) throw new Error("cannot load " + path + ": run ekin tool tts first");
    return res.json();
  }

  /**
   * Karaoke captions. words = timings.words. Groups words into short
   * lines (maxWords, broken after sentence punctuation) and shows the
   * line being spoken with the current word highlighted (class "on").
   * Returns update(t).
   */
  function captions(el, words, opts = {}) {
    const maxWords = opts.maxWords || 5;
    const hold = opts.holdSeconds ?? 0.6;
    const groups = [];
    let cur = [];
    for (const w of words) {
      cur.push(w);
      if (cur.length >= maxWords || /[.!?…:;]$/.test(w.text)) { groups.push(cur); cur = []; }
    }
    if (cur.length) groups.push(cur);
    el.classList.add("ekin-captions", "kurgu-captions"); // kurgu-: compositions written before the rename
    let shown = -1;
    return function update(t) {
      let gi = -1;
      for (let i = 0; i < groups.length; i++) {
        const g = groups[i];
        const end = i + 1 < groups.length ? Math.min(groups[i + 1][0].start, g[g.length - 1].end + hold) : g[g.length - 1].end + hold;
        if (t >= g[0].start - 0.05 && t < end) { gi = i; break; }
      }
      if (gi !== shown) {
        el.innerHTML = gi < 0 ? "" : groups[gi].map((w) => `<span class="w">${escapeHTML(w.text)}</span>`).join(" ");
        shown = gi;
      }
      if (gi >= 0) {
        const spans = el.children;
        groups[gi].forEach((w, i) => spans[i].classList.toggle("on", t >= w.start));
      }
      return gi;
    };
  }

  function escapeHTML(s) {
    return s.replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
  }

  /**
   * Seekable <video> (muted, stock footage). seek(t) resolves once the
   * frame at t is decoded. Loops when t runs past the clip.
   */
  function video(el) {
    el.muted = true;
    el.preload = "auto";
    el.pause();
    return {
      el,
      async seek(t) {
        if (!el.duration || isNaN(el.duration)) await once(el, "loadedmetadata");
        const target = el.duration > 0 ? t % el.duration : 0;
        if (Math.abs(el.currentTime - target) < 0.001 && el.readyState >= 2) return;
        const done = once(el, "seeked");
        el.currentTime = target;
        await done;
      },
    };
  }

  function once(el, event) {
    return new Promise((resolve) => el.addEventListener(event, resolve, { once: true }));
  }

  /** Scenes: [{start, end, el}] → update(t) shows the active scene(s) and returns local progress per scene. */
  function scenes(list) {
    return function update(t) {
      return list.map((s) => {
        const active = t >= s.start && t < s.end;
        if (s.el) s.el.style.visibility = active ? "visible" : "hidden";
        return { active, p: progress(t, s.start, s.end), local: t - s.start };
      });
    };
  }

  async function preloadImages() {
    const imgs = Array.from(document.images);
    await Promise.all(imgs.map((img) => (img.complete ? img.decode().catch(() => {}) : once(img, "load").then(() => img.decode().catch(() => {})))));
  }

  /**
   * Declares the composition. spec = {width, height, fps, duration,
   * setup?: async () => void, render: (t) => void | Promise<void>}.
   * duration may be a number or an async function (e.g. from timings).
   */
  function composition(spec) {
    const ready = (async () => {
      await document.fonts.ready;
      if (spec.setup) await spec.setup();
      await preloadImages();
      const duration = typeof spec.duration === "function" ? await spec.duration() : spec.duration;
      window.ekin.duration = duration;
    })();
    window.ekin = {
      width: spec.width || 1080,
      height: spec.height || 1920,
      fps: spec.fps || 30,
      duration: typeof spec.duration === "number" ? spec.duration : 0,
      ready,
      async renderAt(t) {
        await ready;
        await spec.render(t);
      },
    };
    return window.ekin;
  }

  /**
   * Declares a thumbnail page (thumbnail.html): one static page that draws
   * any of the video's thumbnail variants. spec = {width = 1280,
   * height = 720, setup?: async (data) => void,
   * render: (variant, data) => void | Promise<void>}. data is
   * thumbnails.json; render(variant) lays out that variant alone, taking
   * its words from variant.text (never typed into the page).
   */
  function thumbnails(spec) {
    let data = null;
    const ready = (async () => {
      const res = await fetch("thumbnails.json");
      if (!res.ok) throw new Error("cannot load thumbnails.json");
      data = await res.json();
      await document.fonts.ready;
      if (spec.setup) await spec.setup(data);
      await preloadImages();
    })();
    window.ekin = {
      width: spec.width || 1280,
      height: spec.height || 720,
      fps: 1,
      duration: 1,
      thumbnailPage: true,
      ready,
      async thumbnail(n) {
        await ready;
        const variant = (data.variants || []).find((v) => v.n === n);
        if (!variant) throw new Error("thumbnails.json has no variant " + n);
        await spec.render(variant, data);
        await document.fonts.ready;
        await preloadImages();
      },
      async renderAt() {
        await ready;
      },
    };
    return window.ekin;
  }

  window.Ekin = { clamp, lerp, progress, ease, tween, random, timings, captions, video, scenes, composition, thumbnails };
  // Compositions written before the product was renamed call Kurgu.* and
  // read window.kurgu; they keep rendering unchanged.
  window.Kurgu = window.Ekin;
  Object.defineProperty(window, "kurgu", { get: () => window.ekin, configurable: true });
})();
