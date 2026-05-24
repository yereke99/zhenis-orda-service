	/* ZHENIS ORDA UNIVERSE — premium Mini App + browser admin frontend.
   Static SPA, no build system. Preserves all existing API contracts. */

(function () {
  "use strict";

  /* ===========================================================
     STATE
     =========================================================== */

  const state = {
    mode: "boot", // "miniapp" | "admin" | "landing" | "boot"
    me: null,
    platform: null,
    levels: [],
    lessons: [],
    books: [],
    freeLessons: [],
    premiumCourses: [],
    referral: null,
    coins: null,
    currentScreen: "dashboard",
    selectedTariff: null,
    selectedBookId: null,
    bookReturnScreen: "dashboard",
	    selectedFreeLessonId: null,
	    freeLessonReturnScreen: "dashboard",
	    selectedLessonId: null,
	    lessonReturnScreen: "lessons",
	    selectedPremiumCourseId: null,
	    premiumCourseReturnScreen: "dashboard",
	    testReturnScreen: "lessons",
	    testResult: null,
	    testSelectedAnswers: {},
	    testResultLevel: 0,
    whatsappSalesPhone: "",
    legalAgreementStatus: null,
    financialIqAnswers: {},
    financialIqResult: null,
    financialIqReturnScreen: "dashboard",
	    adminScreen: "dashboard",
	    admin: null,
	    adminUsers: [],
	    adminPayments: [],
	    adminUserSearch: "",
	    adminUsersLoading: false,
	    adminPaymentsLoading: false,
	    adminPaymentManualID: "",
	    adminPaymentManualComment: "",
	    adminPolling: { screen: "", timer: null, loading: false, lastWarningAt: 0 },
		    fullscreenRequested: false,
	    telegramViewportSetup: false,
	    telegramBackHandlerBound: false,
	    bootedAt: 0,
	  };

  const TELEGRAM_INIT_WAIT_MS = 2200;
  const TELEGRAM_INIT_RETRY_MS = 1200;
  const TELEGRAM_AUTH_REQUIRED_MESSAGE =
    "Telegram авторизациясы қажет. Mini App-ты Telegram ішінен ашыңыз.";
  const TELEGRAM_AUTH_FAILED_MESSAGE =
    "Telegram авторизациясы сәтсіз аяқталды. Mini App-ты бот ішіндегі батырма арқылы қайта ашыңыз.";
  const DEFAULT_CHANNEL_LINK = "https://t.me/zhenisOrdaFinanceBot";
  const WHATSAPP_SUPPORT_NUMBER = "77013717776";
  const WHATSAPP_SUPPORT_MESSAGE = "Сәлеметсіз бе! ZHENIS ORDA бойынша көмек керек.";
  const WHATSAPP_SUPPORT_URL = `https://wa.me/${WHATSAPP_SUPPORT_NUMBER}?text=${encodeURIComponent(WHATSAPP_SUPPORT_MESSAGE)}`;
  const DEFAULT_PAYMENT_PROVIDER = "kaspi_qr";
  let certificatePopoverNode = null;
  let certificatePopoverOutsideHandler = null;

  /* ===========================================================
     DOM ELEMENT REGISTRY
     =========================================================== */

  const els = {};

  function cacheElements() {
    [
      "miniApp",
      "userHeader",
      "tgAvatar",
      "tgAvatarFallback",
      "tgName",
      "tgLogin",
      "profileAction",
      "appContent",
      "bottomCta",
      "adminAuth",
      "adminLoginForm",
      "adminPassword",
      "adminLoginError",
      "adminLoginSubmit",
      "adminLoginHint",
      "togglePassword",
      "adminApp",
      "adminSidebar",
      "adminNav",
      "adminNavToggle",
      "adminTitle",
      "adminWho",
      "adminLogout",
      "adminLogoutTop",
      "adminContent",
      "landing",
      "landingTelegramLink",
      "toastStack",
      "modalRoot",
    ].forEach((id) => {
      els[id] = document.getElementById(id);
    });
  }

  /* ===========================================================
     TELEGRAM HELPERS
     =========================================================== */

  function getTelegram() {
    return window.Telegram && window.Telegram.WebApp ? window.Telegram.WebApp : null;
  }

  function getTelegramInitData() {
    const tg = getTelegram();
    return tg && typeof tg.initData === "string" ? tg.initData : "";
  }

  function hasTelegramInitData() {
    return getTelegramInitData().trim().length > 0;
  }

  function hasTelegramUnsafeUser(tg) {
    return Boolean(tg && tg.initDataUnsafe && tg.initDataUnsafe.user);
  }

  function isTelegramMiniAppLaunch() {
    const tg = getTelegram();
    return Boolean(tg && tg.initData);
  }

  function isDevMiniAppLaunch() {
    return new URLSearchParams(window.location.search).get("miniapp_dev") === "1";
  }

  function isDevelopmentDebug() {
    const host = window.location.hostname;
    return (
      isDevMiniAppLaunch() ||
      host === "localhost" ||
      host === "127.0.0.1" ||
      host === "" ||
      new URLSearchParams(window.location.search).get("tg_debug") === "1"
    );
  }

  function logTelegramDebug(stage) {
    if (!isDevelopmentDebug() || !window.console || typeof window.console.debug !== "function") {
      return;
    }
    const tg = getTelegram();
    window.console.debug("[ZHENIS ORDA Telegram]", stage, {
      hasTelegram: Boolean(window.Telegram),
      hasWebApp: Boolean(tg),
      hasUnsafeUser: hasTelegramUnsafeUser(tg),
      initDataLength: getTelegramInitData().length,
      platform: (tg && tg.platform) || "",
      version: (tg && tg.version) || "",
      viewportHeight: (tg && tg.viewportHeight) || 0,
      viewportStableHeight: (tg && tg.viewportStableHeight) || 0,
      colorScheme: (tg && tg.colorScheme) || "",
    });
  }

  function telegramVersionAtLeast(tg, version) {
    if (!tg) return false;
    if (typeof tg.isVersionAtLeast === "function") {
      try {
        return tg.isVersionAtLeast(version);
      } catch (_) {}
    }
    const parse = (value) =>
      String(value || "")
        .split(".")
        .map((part) => Number(part) || 0);
    const current = parse(tg.version);
    const required = parse(version);
    const length = Math.max(current.length, required.length);
    for (let i = 0; i < length; i += 1) {
      const a = current[i] || 0;
      const b = required[i] || 0;
      if (a > b) return true;
      if (a < b) return false;
    }
    return true;
  }

  function sleep(ms) {
    return new Promise((resolve) => window.setTimeout(resolve, ms));
  }

  async function waitForTelegramInitData(timeoutMs) {
    const started = Date.now();
    let lastTg = getTelegram();
    let sawWebApp = Boolean(lastTg);

    while (Date.now() - started <= timeoutMs) {
      lastTg = getTelegram();
      if (lastTg) {
        sawWebApp = true;
        try {
          if (typeof lastTg.ready === "function") lastTg.ready();
        } catch (_) {}
        try {
          if (typeof lastTg.expand === "function") lastTg.expand();
        } catch (_) {}
      }
      if (hasTelegramInitData()) {
        logTelegramDebug("initData ready");
        return { hasInitData: true, sawWebApp, tg: lastTg };
      }
      await sleep(80);
    }

    logTelegramDebug("initData missing after wait");
    return { hasInitData: false, sawWebApp, tg: lastTg };
  }

  function onTelegramEvent(name, handler) {
    const tg = getTelegram();
    if (!tg || typeof tg.onEvent !== "function") return;
    try {
      tg.onEvent(name, handler);
    } catch (_) {
      // not supported in this Telegram version
    }
  }

  /* ===========================================================
     VIEWPORT / FORCE EXPAND
     =========================================================== */

  function setupTelegramViewport() {
    forceExpand();

    onTelegramEvent("viewportChanged", syncTelegramFrame);
    onTelegramEvent("safeAreaChanged", updateViewportVars);
    onTelegramEvent("contentSafeAreaChanged", updateViewportVars);
    onTelegramEvent("fullscreenChanged", () => {
      const tg = getTelegram();
      if (tg && tg.isFullscreen) {
        document.documentElement.classList.add("telegram-fullscreen");
      } else {
        document.documentElement.classList.remove("telegram-fullscreen");
      }
      updateViewportVars();
    });
    onTelegramEvent("fullscreenFailed", syncTelegramFrame);
    onTelegramEvent("activated", syncTelegramFrame);

    window.addEventListener("resize", syncTelegramFrame, { passive: true });
    window.addEventListener("orientationchange", syncTelegramFrame, { passive: true });
    window.addEventListener("focus", syncTelegramFrame);

    if (window.visualViewport) {
      window.visualViewport.addEventListener("resize", syncTelegramFrame);
      window.visualViewport.addEventListener("scroll", syncTelegramFrame);
    }

    document.addEventListener("visibilitychange", () => {
      if (!document.hidden) syncTelegramFrame();
    });
  }

  function forceExpand() {
    const webApp = getTelegram();

    if (!webApp) {
      updateViewportVars();
      return;
    }

    try {
      webApp.ready();
    } catch (_) {}
    try {
      webApp.expand();
    } catch (_) {}
    if (telegramVersionAtLeast(webApp, "6.1")) {
      try {
        if (typeof webApp.setBackgroundColor === "function") webApp.setBackgroundColor("#07090E");
        if (typeof webApp.setHeaderColor === "function") webApp.setHeaderColor("#07090E");
      } catch (_) {}
    }
    if (telegramVersionAtLeast(webApp, "7.7")) {
      try {
        if (typeof webApp.disableVerticalSwipes === "function") webApp.disableVerticalSwipes();
      } catch (_) {}
    }

    if (telegramVersionAtLeast(webApp, "8.0") && typeof webApp.requestFullscreen === "function" && !webApp.isFullscreen) {
      try {
        webApp.requestFullscreen();
        state.fullscreenRequested = true;
        document.documentElement.classList.add("telegram-fullscreen");
      } catch (_) {
        /* fallback to expand only */
      }
    }

    syncTelegramFrame();
    window.setTimeout(syncTelegramFrame, 120);
    window.setTimeout(syncTelegramFrame, 450);
    window.setTimeout(syncTelegramFrame, 900);
  }

  function syncTelegramFrame() {
    updateViewportVars();
    const tg = getTelegram();
    if (tg) {
      try {
        tg.expand();
      } catch (_) {}
    }
  }

  function updateViewportVars() {
    const tg = getTelegram();
    const browserHeight = window.innerHeight || document.documentElement.clientHeight || 0;
    const viewportWidth =
      (window.visualViewport && window.visualViewport.width) ||
      window.innerWidth ||
      document.documentElement.clientWidth ||
      0;
    const viewportHeight = tg && tg.viewportHeight ? tg.viewportHeight : browserHeight;
    const stableHeight = tg && tg.viewportStableHeight ? tg.viewportStableHeight : viewportHeight;

    const safe = (tg && tg.safeAreaInset) || {};
    const contentSafe = (tg && tg.contentSafeAreaInset) || {};

    const safeTop = Number(safe.top) || 0;
    const safeBottom = Number(safe.bottom) || 0;
    const contentTop = Number(contentSafe.top) || 0;
    const contentBottom = Number(contentSafe.bottom) || 0;

    const top = getAppTopPadding(stableHeight, safeTop, contentTop);
    const bottom = Math.max(14, safeBottom + contentBottom + 12);

    const root = document.documentElement;
    root.style.setProperty("--viewport-width", `${Math.round(viewportWidth)}px`);
    root.style.setProperty("--browser-viewport-height", `${Math.round(browserHeight)}px`);
    root.style.setProperty("--viewport-height", `${Math.round(viewportHeight)}px`);
    root.style.setProperty("--viewport-stable-height", `${Math.round(stableHeight)}px`);
    root.style.setProperty("--tg-safe-area-inset-top", `${safeTop}px`);
    root.style.setProperty("--tg-safe-area-inset-bottom", `${safeBottom}px`);
    root.style.setProperty("--tg-content-safe-area-inset-top", `${contentTop}px`);
    root.style.setProperty("--tg-content-safe-area-inset-bottom", `${contentBottom}px`);
    root.style.setProperty("--app-top-padding", `${top}px`);
    root.style.setProperty("--app-bottom-padding", `${bottom}px`);
  }

  function getAppTopPadding(stableHeight, safeTop, contentTop) {
    const webApp = getTelegram();
    if (!webApp) return Math.max(16, safeTop + 12, contentTop + 12);

    const platform = String(webApp.platform || "").toLowerCase();
    const coarse =
      typeof window.matchMedia === "function" &&
      window.matchMedia("(pointer: coarse)").matches;

    const looksMobile =
      /ios|android|mobile/.test(platform) ||
      (window.innerWidth <= 640 && coarse);

    const fullscreen = Boolean(webApp.isFullscreen || state.fullscreenRequested);
    const contentReserve = contentTop > 0 ? contentTop + 12 : 0;
    const safeReserve = safeTop + 14;

    const systemButtonReserve = looksMobile ? Math.max(72, safeTop + 44) : 18;

    const fullscreenReserve =
      looksMobile && fullscreen
        ? Math.min(118, Math.max(82, Math.round(stableHeight * 0.1)))
        : 0;

    return Math.max(16, safeReserve, contentReserve, systemButtonReserve, fullscreenReserve);
  }

  /* ===========================================================
     PROFILE / HEADER RENDERING
     =========================================================== */

  function readTelegramUser() {
    const tg = getTelegram();
    return (tg && tg.initDataUnsafe && tg.initDataUnsafe.user) || {};
  }

  function pick(...values) {
    for (const v of values) {
      if (v !== undefined && v !== null && String(v).trim() !== "") return v;
    }
    return "";
  }

  function mergeDisplayUser(tgUser, apiUser) {
    const user = Object.assign({}, apiUser || {});
    Object.entries(tgUser || {}).forEach(([key, value]) => {
      if (pick(value)) user[key] = value;
    });
    return user;
  }

  function buildDisplayName(tgUser, apiUser) {
    const user = mergeDisplayUser(tgUser, apiUser);
    const first = pick(
      user.first_name,
      user.firstName,
    );
    const last = pick(
      user.last_name,
      user.lastName,
    );
    const username = pick(
      user.username,
      user.user_name,
      user.telegram_username,
    );
    const composed = `${first} ${last}`.trim();
    if (composed) return composed;
    if (username) return username;
    return "ZHENIS ORDA";
  }

  function buildLogin(tgUser, apiUser) {
    const user = mergeDisplayUser(tgUser, apiUser);
    const username = pick(
      user.username,
      user.user_name,
      user.telegram_username,
    );
    if (username) return `@${String(username).replace(/^@/, "")}`;

    const tid = pick(
      user.id,
      user.telegram_id,
      user.telegramId,
    );
    if (tid) return `ID ${tid}`;

    return "Telegram қолданушысы";
  }

  function buildPhotoUrl(tgUser, apiUser) {
    const user = mergeDisplayUser(tgUser, apiUser);
    return (
      pick(
        user.photo_url,
        user.photoUrl,
      ) || ""
    );
  }

  function renderHeader() {
    if (!els.userHeader) return;
    const tgUser = readTelegramUser();
    const apiUser = state.me && state.me.user ? state.me.user : null;

    const name = buildDisplayName(tgUser, apiUser);
    const login = buildLogin(tgUser, apiUser);
    const photo = buildPhotoUrl(tgUser, apiUser);

    els.tgName.textContent = name;
    els.tgLogin.textContent = login;

    const initial = (name || "ZO").trim().charAt(0).toUpperCase() || "Z";
    els.tgAvatarFallback.textContent = initial === "Z" ? "ZO" : initial;

    if (photo) {
      els.tgAvatar.src = photo;
      els.tgAvatar.onload = () => {
        els.tgAvatar.classList.remove("hidden");
        els.tgAvatarFallback.classList.add("hidden");
      };
      els.tgAvatar.onerror = () => {
        els.tgAvatar.classList.add("hidden");
        els.tgAvatarFallback.classList.remove("hidden");
      };
    } else {
      els.tgAvatar.classList.add("hidden");
      els.tgAvatar.removeAttribute("src");
      els.tgAvatarFallback.classList.remove("hidden");
    }
  }

  /* ===========================================================
     API
     =========================================================== */

  function api(path, options) {
    options = options || {};
    const initData = getTelegramInitData();
    const isMultipart = typeof FormData !== "undefined" && options.body instanceof FormData;
    const headers = new Headers(options.headers || {});
    if (!isMultipart && !headers.has("Content-Type")) headers.set("Content-Type", "application/json");
    if (initData) headers.set("X-Telegram-Init-Data", initData);
    if (isDevMiniAppLaunch()) {
      const query = new URLSearchParams(window.location.search);
      headers.set("X-Miniapp-Dev", "1");
      if (query.get("telegram_id")) headers.set("X-Dev-Telegram-ID", query.get("telegram_id"));
      if (query.get("username")) headers.set("X-Dev-Username", query.get("username"));
      if (query.get("first_name")) headers.set("X-Dev-First-Name", query.get("first_name"));
      if (query.get("last_name")) headers.set("X-Dev-Last-Name", query.get("last_name"));
      if (query.get("photo_url")) headers.set("X-Dev-Photo-URL", query.get("photo_url"));
    }
    const finalOpts = Object.assign({}, options, {
      headers,
      credentials: "include",
    });
    return fetch(path, finalOpts).then((res) => {
      if (path === "/api/me" && isDevelopmentDebug() && window.console) {
        window.console.debug("[ZHENIS ORDA Telegram]", "/api/me", { status: res.status });
      }
      return res
        .json()
        .catch(() => ({}))
        .then((body) => {
          if (!res.ok) {
            const err = new Error(body.error || `HTTP ${res.status}`);
            err.status = res.status;
            err.body = body;
            throw err;
          }
          return body;
        });
    });
  }

  /* ===========================================================
     UTILITIES
     =========================================================== */

  function esc(value) {
    const map = {
      "&": "&amp;",
      "<": "&lt;",
      ">": "&gt;",
      '"': "&quot;",
      "'": "&#39;",
    };
    return String(value == null ? "" : value).replace(/[&<>"']/g, (ch) => map[ch]);
  }

  function num(value) {
    return Number(value || 0);
  }

  function money(value) {
    return num(value).toLocaleString("kk-KZ");
  }

  function shortId(value) {
    const str = String(value || "");
    return str.length > 10 ? `${str.slice(0, 8)}…` : str || "—";
  }

	  function clean(value) {
	    if (value == null) return "";
	    return String(value);
	  }

  function compact(value) {
    return clean(value).trim();
  }

	  function setButtonLoading(button, loading) {
	    if (!button) return;
	    button.classList.toggle("is-loading", Boolean(loading));
	    button.disabled = Boolean(loading);
	  }

	  function setModalBusy(shell, loading) {
	    if (!shell || !shell.backdrop) return;
	    shell.backdrop.dataset.busy = loading ? "1" : "";
	    shell.body.querySelectorAll("[data-close-modal]").forEach((button) => {
	      button.disabled = Boolean(loading);
	    });
	  }

	  function buttonIsLoading(button) {
	    return Boolean(button && button.classList.contains("is-loading"));
	  }

	  function isValidTelegramLink(value) {
	    const raw = compact(value);
	    if (!raw) return true;
	    try {
	      const url = new URL(raw);
	      const host = url.hostname.toLowerCase();
	      return url.protocol === "https:" && (host === "t.me" || host === "telegram.me") && url.pathname.length > 1;
	    } catch (_) {
	      return false;
	    }
	  }

	  function isValidHTTPURL(value) {
	    const raw = compact(value);
	    if (!raw) return true;
	    try {
	      const url = new URL(raw);
	      return url.protocol === "https:" || url.protocol === "http:";
	    } catch (_) {
	      return false;
	    }
	  }

	  function parseYouTubeVideoID(value) {
	    const raw = compact(value);
	    if (!raw) return "";
	    try {
	      const url = new URL(raw);
	      const host = url.hostname.toLowerCase();
	      let id = "";
	      if (host === "youtu.be") {
	        id = url.pathname.split("/").filter(Boolean)[0] || "";
	      } else if (host === "youtube.com" || host.endsWith(".youtube.com")) {
	        const parts = url.pathname.split("/").filter(Boolean);
	        if (parts[0] === "watch") id = url.searchParams.get("v") || "";
	        if ((parts[0] === "shorts" || parts[0] === "embed") && parts[1]) id = parts[1];
	      } else if (host === "youtube-nocookie.com" || host.endsWith(".youtube-nocookie.com")) {
	        const parts = url.pathname.split("/").filter(Boolean);
	        if (parts[0] === "embed" && parts[1]) id = parts[1];
	      }
	      return /^[A-Za-z0-9_-]{11}$/.test(id) ? id : "";
	    } catch (_) {
	      return "";
	    }
	  }

	  function youtubeEmbedURLFromID(videoID) {
	    const id = compact(videoID);
	    if (!/^[A-Za-z0-9_-]{11}$/.test(id)) return "";
	    const params = new URLSearchParams({
	      rel: "0",
	      modestbranding: "1",
	      playsinline: "1",
	      controls: "1",
	      fs: "0",
	      iv_load_policy: "3",
	      disablekb: "1",
	    });
	    return `https://www.youtube-nocookie.com/embed/${encodeURIComponent(id)}?${params.toString()}`;
	  }

	  function youtubeEmbedURL(lesson) {
	    const id =
	      compact(lesson && lesson.youtube_video_id) ||
	      parseYouTubeVideoID(lesson && lesson.youtube_url) ||
	      parseYouTubeVideoID(lesson && lesson.youtube_embed_url);
	    return youtubeEmbedURLFromID(id);
	  }

	  function youtubeIframe(src, title) {
	    if (!src) return "";
	    // YouTube controls, branding, and in-player links are cross-origin UI owned by YouTube.
	    // Keep this embed sandboxed and popup-free; use direct video/HLS hosting for full control.
	    return `<iframe src="${esc(src)}" title="${esc(title || "YouTube")}" loading="lazy" referrerpolicy="strict-origin-when-cross-origin" sandbox="allow-scripts allow-same-origin allow-presentation" allow="autoplay; encrypted-media"></iframe>`;
	  }

	  function visibleTariffImage(tariff) {
	    if (!tariff) return "";
	    return compact(tariff.image_file_path) || compact(tariff.image_url);
	  }

	  function visibleFreeLessonImage(lesson) {
	    if (!lesson) return "";
	    return compact(lesson.image_file_path) || compact(lesson.image_url);
	  }

	  function visibleBookImage(book) {
	    if (!book) return "";
	    return compact(book.image_file_path) || compact(book.image_url);
	  }

	  function visiblePremiumCourseImage(course) {
	    if (!course) return "";
	    return compact(course.cover_image_path) || compact(course.cover_image_url);
	  }

	  function shortText(value, limit) {
	    const text = compact(value).replace(/\s+/g, " ");
	    const max = limit || 120;
	    return text.length > max ? `${text.slice(0, max - 1).trim()}…` : text;
	  }

	  function bookParagraphs(value) {
	    return compact(value)
	      .split(/\n{2,}/)
	      .map((paragraph) => paragraph.trim())
	      .filter(Boolean)
	      .map((paragraph) => `<p class="muted">${esc(paragraph)}</p>`)
	      .join("");
	  }

	  function bookBuyUrl(book) {
	    const phone = compact(state.whatsappSalesPhone).replace(/\D/g, "");
	    if (!phone || !book) return "";
	    const message = `Сәлеметсіз бе! Мен «${book.title || ""}» кітабын сатып алғым келеді. Бағасы: ${money(book.price_kzt)} ₸.`;
	    return `https://wa.me/${encodeURIComponent(phone)}?text=${encodeURIComponent(message)}`;
	  }

	  function premiumCourseManagerUrl(course) {
	    const phone = compact(state.whatsappSalesPhone).replace(/\D/g, "");
	    if (!phone || !course) return "";
	    const message = `Сәлеметсіз бе! Мен «${course.title || ""}» premium курсына қолжетімділік алғым келеді. Бағасы: ${money(course.price_kzt)} ₸.`;
	    return `https://wa.me/${encodeURIComponent(phone)}?text=${encodeURIComponent(message)}`;
	  }

	  function openExternalLink(url, emptyMessage) {
	    if (!url) {
	      toast(emptyMessage || "Сілтеме бапталмаған", "error");
	      return;
	    }
	    const tg = getTelegram();
	    if (tg && typeof tg.openLink === "function") {
	      tg.openLink(url);
	      return;
	    }
	    window.open(url, "_blank", "noopener,noreferrer");
	  }

	  function openWhatsAppSupport() {
	    openExternalLink(WHATSAPP_SUPPORT_URL, "WhatsApp сілтемесі қолжетімді емес");
	  }

	  function currentUserPhone() {
	    return compact(state.me && state.me.user && state.me.user.phone);
	  }

	  function kzPhoneTypingDigits(value) {
	    let digits = String(value == null ? "" : value).replace(/\D/g, "");
	    if (!digits) return "";
	    if (digits[0] === "8") digits = "7" + digits.slice(1);
	    if (digits.length > 11) digits = digits.slice(0, 11);
	    return digits;
	  }

	  function kzPhoneCanonicalDigits(value) {
	    let digits = kzPhoneTypingDigits(value);
	    if (digits.length === 10) digits = "7" + digits;
	    if (digits.length > 11) digits = digits.slice(0, 11);
	    return digits;
	  }

	  function formatKzPhoneDigits(digits) {
	    if (!digits) return "";
	    if (digits[0] !== "7") return "+" + digits;
	    const rest = digits.slice(1);
	    let out = "+7";
	    if (rest.length) out += " " + rest.slice(0, Math.min(3, rest.length));
	    if (rest.length > 3) out += " " + rest.slice(3, Math.min(6, rest.length));
	    if (rest.length > 6) out += " " + rest.slice(6, Math.min(8, rest.length));
	    if (rest.length > 8) out += " " + rest.slice(8, Math.min(10, rest.length));
	    return out;
	  }

	  function formatKzPhone(value) {
	    return formatKzPhoneDigits(kzPhoneTypingDigits(value));
	  }

	  function isValidKzPhone(value) {
	    const clean = kzPhoneCanonicalDigits(value);
	    return clean.length === 11 && clean[0] === "7";
	  }

	  function normalizedKzPhone(value) {
	    const clean = kzPhoneCanonicalDigits(value);
	    return clean ? "+" + clean : "";
	  }

	  function bindKzPhoneInput(input) {
	    if (!input) return;
	    const reformat = () => {
	      const raw = input.value;
	      const selStart = input.selectionStart == null ? raw.length : input.selectionStart;
	      let digitsBefore = 0;
	      for (let i = 0; i < selStart && i < raw.length; i++) {
	        if (/\d/.test(raw[i])) digitsBefore++;
	      }
	      const digits = kzPhoneTypingDigits(raw);
	      const formatted = formatKzPhoneDigits(digits);
	      if (formatted === raw && input.selectionStart === selStart) return;
	      input.value = formatted;
	      let pos = formatted.length;
	      if (digitsBefore < digits.length) {
	        let seen = 0;
	        for (let i = 0; i < formatted.length; i++) {
	          if (/\d/.test(formatted[i])) {
	            seen++;
	            if (seen > digitsBefore) {
	              pos = i;
	              break;
	            }
	          }
	        }
	      }
	      try {
	        input.setSelectionRange(pos, pos);
	      } catch (_) {}
	    };
	    if (input.value) input.value = formatKzPhone(input.value);
	    input.addEventListener("input", reformat);
	    input.addEventListener("paste", () => {
	      setTimeout(reformat, 0);
	    });
	  }

	  function staticKaspiMethodHtml() {
	    return `
	      <div class="field payment-method-field">
	        <span>Төлем тәсілі</span>
	        <div class="payment-method-static" aria-label="Kaspi">
	          <span class="payment-method-static-mark" aria-hidden="true">K</span>
	          <div class="payment-method-static-meta">
	            <strong>Kaspi</strong>
	            <small>Kaspi арқылы қауіпсіз төлем</small>
	          </div>
	        </div>
	      </div>
	    `;
	  }

	  function paymentProviderURL(instructions, provider) {
	    const data = instructions || {};
	    switch (provider) {
	      case "kaspi_pay":
	        return compact(data.kaspi_pay_url);
	      case "kaspi_qr":
	        return compact(data.kaspi_pay_url) || compact(data.kaspi_qr_image_url);
	      case "halyk":
	        return compact(data.halyk_payment_url);
	      case "bank_card":
	        return compact(data.bank_card_url);
	      default:
	        return compact(data.kaspi_pay_url);
	    }
	  }

	  function openPurchasePhoneSheet(product) {
	    return new Promise((resolve) => {
	      const backdrop = document.createElement("div");
	      backdrop.className = "sheet-backdrop";
	      const sheet = document.createElement("div");
	      sheet.className = "purchase-sheet";
	      const savedPhone = currentUserPhone();
	      const initialPhone = savedPhone ? formatKzPhoneDigits(kzPhoneCanonicalDigits(savedPhone)) : "";
	      sheet.innerHTML = `
	        <div class="sheet-handle" aria-hidden="true"></div>
	        <div class="sheet-head">
	          <div>
	            <p class="eyebrow">Төлемге дайындық</p>
	            <h2>Төлемге дайындық</h2>
	          </div>
	          <button class="sheet-close" type="button" aria-label="Жабу">×</button>
	        </div>
	        <p class="muted">Тарифті рәсімдеу үшін байланыс нөміріңізді растаңыз.</p>
	        <p class="sheet-product">${esc(product && product.title ? product.title : "Тариф")} · ${money(product && product.amount)} ₸</p>
	        <form class="form purchase-phone-form" novalidate>
	          <label class="field">
	            <span>Байланыс нөмірі</span>
	            <input name="contact_phone" type="tel" inputmode="tel" autocomplete="tel" placeholder="+7 747 185 04 99" value="${esc(initialPhone)}" maxlength="18" required />
	          </label>
	          <p class="muted small">Нөмірді растаңыз немесе өзгертіңіз.</p>
	          <div class="sheet-error" role="alert"></div>
	          <div class="action-row sheet-actions">
	            <button class="ghost-btn" data-close-sheet type="button">Жабу</button>
	            <button class="gold-btn" type="submit">Жалғастыру</button>
	          </div>
	        </form>
	      `;
	      const close = (value) => {
	        backdrop.remove();
	        resolve(value);
	      };
	      const input = sheet.querySelector("input[name=contact_phone]");
	      const errorNode = sheet.querySelector(".sheet-error");
	      bindKzPhoneInput(input);
	      const clearError = () => {
	        if (errorNode.textContent) errorNode.textContent = "";
	        input.classList.remove("has-error");
	      };
	      input.addEventListener("input", clearError);
	      sheet.querySelector(".sheet-close").addEventListener("click", () => close(null));
	      sheet.querySelector("[data-close-sheet]").addEventListener("click", () => close(null));
	      sheet.querySelector("form").addEventListener("submit", (event) => {
	        event.preventDefault();
	        if (!isValidKzPhone(input.value)) {
	          errorNode.textContent = "Телефон нөмірін дұрыс енгізіңіз.";
	          input.classList.add("has-error");
	          input.focus();
	          return;
	        }
	        close(normalizedKzPhone(input.value));
	      });
	      backdrop.addEventListener("click", (event) => {
	        if (event.target === backdrop) close(null);
	      });
	      backdrop.appendChild(sheet);
	      els.modalRoot.appendChild(backdrop);
	      setTimeout(() => {
	        input.focus();
	        try {
	          const end = input.value.length;
	          input.setSelectionRange(end, end);
	        } catch (_) {}
	      }, 80);
	    });
	  }

	  function openPaymentInstructionSheet(payment, instructions, provider) {
	    return new Promise((resolve) => {
	      const backdrop = document.createElement("div");
	      backdrop.className = "sheet-backdrop";
	      const sheet = document.createElement("div");
	      sheet.className = "purchase-sheet instruction-sheet";
	      sheet.innerHTML = `
	        <div class="sheet-handle" aria-hidden="true"></div>
	        <div class="sheet-head">
	          <div>
	            <p class="eyebrow">Төлем нұсқаулығы</p>
	            <h2>Төлем нұсқаулығы</h2>
	          </div>
	          <button class="sheet-close" type="button" aria-label="Жабу">×</button>
	        </div>
	        <ol class="instruction-list">
	          <li>Kaspi арқылы төлем жасаңыз.</li>
	          <li>Төлемнен кейін PDF-чекті ашыңыз.</li>
	          <li>«Бөлісу» батырмасын басып, PDF-чекті Telegram ботқа жіберіңіз.</li>
	          <li>Чек тексерілгеннен кейін курсқа қолжетімділік ашылады.</li>
	        </ol>
	        <p class="sheet-note">Маңызды: чек сомасы тариф бағасына сәйкес болуы керек.</p>
	        <p class="sheet-product">Сома: ${money(payment && payment.amount_kzt)} ₸</p>
	        <div class="action-row sheet-actions">
	          <button class="ghost-btn" data-close-sheet type="button">Жабу</button>
	          <button class="gold-btn" data-pay-kaspi type="button">Kaspi арқылы төлеу</button>
	        </div>
	      `;
	      const close = (value) => {
	        backdrop.remove();
	        resolve(value);
	      };
	      sheet.querySelector(".sheet-close").addEventListener("click", () => close(false));
	      sheet.querySelector("[data-close-sheet]").addEventListener("click", () => close(false));
	      sheet.querySelector("[data-pay-kaspi]").addEventListener("click", () => close(true));
	      backdrop.addEventListener("click", (event) => {
	        if (event.target === backdrop) close(false);
	      });
	      backdrop.appendChild(sheet);
	      els.modalRoot.appendChild(backdrop);
	    }).then((shouldOpen) => {
	      if (shouldOpen) {
	        openExternalLink(paymentProviderURL(instructions, provider), "Төлем сілтемесі бапталмаған");
	      }
	      return shouldOpen;
	    });
	  }

	  function selectedTariff() {
	    const tariffs = (state.platform && state.platform.tariffs) || [];
	    const selected = compact(state.selectedTariff);
	    return tariffs.find((item) => item.id === selected || item.code === selected) || tariffs[0] || null;
	  }

	  function selectedPremiumCourse() {
	    const courses = state.premiumCourses || [];
	    const selected = compact(state.selectedPremiumCourseId);
	    return courses.find((item) => item.id === selected) || null;
	  }

		  function copyText(value) {
		    const text = clean(value);
	    const fallback = () => {
	      const input = document.createElement("textarea");
	      input.value = text;
	      input.setAttribute("readonly", "readonly");
	      input.style.position = "fixed";
		      input.style.left = "-9999px";
		      input.style.top = "0";
		      input.style.opacity = "0";
	      document.body.appendChild(input);
	      input.focus();
	      input.select();
	      input.setSelectionRange(0, input.value.length);
	      try {
	        const ok = document.execCommand("copy");
	        return ok ? Promise.resolve() : Promise.reject(new Error("copy failed"));
	      } finally {
	        input.remove();
	      }
	    };
		    if (navigator.clipboard && navigator.clipboard.writeText) {
		      return navigator.clipboard.writeText(text).catch(fallback);
		    }
		    return fallback();
		  }

  function html(markup) {
    if (els.appContent) els.appContent.innerHTML = markup;
  }

  function on(idOrEl, handler, evt) {
    const el = typeof idOrEl === "string" ? document.getElementById(idOrEl) : idOrEl;
    if (el) el.addEventListener(evt || "click", handler);
  }

  function delegate(root, selector, evt, handler) {
    if (!root) return;
    root.addEventListener(evt, (e) => {
      const target = e.target.closest(selector);
      if (target && root.contains(target)) handler(e, target);
    });
  }

  function formatDate(value) {
    if (!value) return "—";
    const d = new Date(value);
    if (Number.isNaN(d.getTime())) return "—";
    return d.toLocaleDateString("kk-KZ", { day: "2-digit", month: "short", year: "numeric" });
  }

  function formatDateTime(value) {
    if (!value) return "—";
    const d = new Date(value);
    if (Number.isNaN(d.getTime())) return "—";
    return d.toLocaleString("kk-KZ", {
      day: "2-digit",
      month: "short",
      hour: "2-digit",
      minute: "2-digit",
    });
  }

  const adminLabels = {
    id: "ID",
    user_id: "Қолданушы",
    telegram_id: "Telegram ID",
    username: "Username",
    first_name: "Аты",
    current_level: "Деңгей",
    coin_balance: "Coin",
    access_closed: "Қолжетімділік",
	    tariff_code: "Тариф",
	    code: "Код",
		    price_kzt: "Баға",
		    description: "Сипаттама",
		    short_description: "Қысқаша сипаттама",
		    short_description_kk: "Қысқа сипаттама",
	    full_description_kk: "Толық сипаттама",
	    image_url: "Сурет URL",
		    image_file_path: "Жүктелген сурет",
		    image_source: "Сурет түрі",
		    youtube_url: "YouTube сілтемесі",
		    youtube_video_id: "YouTube video ID",
		    youtube_embed_url: "YouTube embed",
    amount_kzt: "Сома",
    contact_phone: "Байланыс нөмірі",
    provider: "Провайдер",
    status: "Статус",
    receipt_file_path: "Түбіртек",
    expires_at: "Аяқталуы",
    number: "Деңгей",
    title_kk: "Қазақша атауы",
    title_ru: "Орысша атауы",
	    level_number: "Деңгей",
	    lesson_id: "Сабақ",
	    lesson_title_kk: "Сабақ",
    lesson_link: "Сабақ сілтемесі",
    video_url: "Сілтеме",
    sort_order: "Реті",
    is_active: "Статус",
    title: "Атауы",
    telegram_chat_id: "Telegram chat ID",
    level_requirement: "Деңгей талабы",
    starts_at: "Басталуы",
    tariff_requirement: "Тариф талабы",
    role: "Рөл",
    action: "Әрекет",
    entity_type: "Объект",
    created_at: "Құрылған уақыты",
    pass_percent: "Өту пайызы",
    question_count: "Сұрақ саны",
    sent_count: "Жіберілді",
    failed_count: "Қате",
    target: "Аудитория",
  };

  const statusText = {
    active: "Белсенді",
    inactive: "Белсенді емес",
    pending: "Кезекте",
    uploaded_receipt: "Түбіртек жүктелді",
    approved: "Қабылданды",
    rejected: "Қабылданбады",
    expired: "Мерзімі өтті",
    cancelled: "Болдырмады",
    queued: "Кезекте",
    processing: "Жіберіліп жатыр",
    completed: "Аяқталды",
    failed: "Қате",
    manual: "Қолмен",
    payment: "Төлем",
    bonus: "Бонус",
    gift: "Сыйлық",
    revoked: "Жабылды",
    archived: "Архив",
    lifetime: "Lifetime",
    "30_days": "30 күн",
    "90_days": "90 күн",
    custom: "Custom",
    valid_candidate: "Тексеруге дайын",
    parse_partial: "Қолмен тексеру қажет",
    parse_failed: "Қолмен тексеру қажет",
    suspicious: "Күдікті түбіртек",
    duplicate: "Қайталанған түбіртек",
    uploaded: "Жүктелді",
	    sent: "Жіберілді",
	    blocked: "Бұғатталған",
	  };

  function statusBadge(value) {
    const raw = String(value == null ? "" : value);
    const label = statusText[raw] || (raw === "true" ? "Белсенді" : raw === "false" ? "Жабық" : raw || "—");
    const kind =
	      ["active", "approved", "completed", "sent", "valid_candidate", "true"].includes(raw)
	        ? "ok"
	        : ["rejected", "expired", "cancelled", "failed", "duplicate", "false", "blocked"].includes(raw)
          ? "bad"
          : "warn";
    return `<span class="status ${kind}">${esc(label)}</span>`;
  }

  function adminLabel(key) {
    return adminLabels[key] || String(key).replace(/_/g, " ");
  }

  /* ===========================================================
     TOAST / MODAL
     =========================================================== */

  function toast(message, kind) {
    if (!els.toastStack) return;
    const node = document.createElement("div");
    node.className = `toast ${kind || ""}`.trim();
    node.innerHTML = `<span class="toast-dot"></span><span>${esc(message)}</span>`;
    els.toastStack.appendChild(node);
    setTimeout(() => {
      node.style.opacity = "0";
      node.style.transition = "opacity 220ms ease";
      setTimeout(() => node.remove(), 240);
    }, 3200);
  }

  function modal({ title, body, actions }) {
    return new Promise((resolve) => {
      const backdrop = document.createElement("div");
      backdrop.className = "modal-backdrop";

      const container = document.createElement("div");
      container.className = "modal";
      container.innerHTML = `
        <div class="modal-head">
          <h2>${esc(title || "")}</h2>
        </div>
        <div class="modal-body">${body || ""}</div>
        <div class="modal-foot"></div>
      `;

      const foot = container.querySelector(".modal-foot");
      (actions || [{ label: "Жабу", value: "ok", primary: true }]).forEach((action) => {
        const btn = document.createElement("button");
        btn.className = action.primary ? "gold-btn" : action.danger ? "danger-btn" : "ghost-btn";
        btn.type = "button";
        btn.textContent = action.label;
        btn.addEventListener("click", () => {
          backdrop.remove();
          resolve(action.value);
        });
        foot.appendChild(btn);
      });

      backdrop.appendChild(container);
      backdrop.addEventListener("click", (e) => {
        if (e.target === backdrop) {
          backdrop.remove();
          resolve(null);
        }
      });
      els.modalRoot.appendChild(backdrop);
    });
  }

	  function confirmAction({ title, body, confirmLabel, cancelLabel, action, successMessage, errorMessage, formatError }) {
	    return new Promise((resolve) => {
	      const backdrop = document.createElement("div");
	      backdrop.className = "modal-backdrop";

	      const container = document.createElement("div");
	      container.className = "modal";
	      container.innerHTML = `
	        <div class="modal-head">
	          <h2>${esc(title || "")}</h2>
	        </div>
	        <div class="modal-body">${body || ""}</div>
	        <div class="modal-foot">
	          <button class="ghost-btn" data-cancel type="button">${esc(cancelLabel || "Болдырмау")}</button>
	          <button class="danger-btn" data-confirm type="button"><span class="btn-label">${esc(confirmLabel || "Өшіру")}</span><span class="btn-spinner"></span></button>
	        </div>
	      `;

	      const close = (value) => {
	        backdrop.remove();
	        resolve(value);
	      };
	      const cancelBtn = container.querySelector("[data-cancel]");
	      const confirmBtn = container.querySelector("[data-confirm]");
	      cancelBtn.addEventListener("click", () => close(false));
	      confirmBtn.addEventListener("click", async () => {
	        if (buttonIsLoading(confirmBtn)) return;
	        setButtonLoading(confirmBtn, true);
	        cancelBtn.disabled = true;
	        try {
	          if (typeof action === "function") await action();
	          if (successMessage) toast(successMessage, "success");
	          close(true);
	        } catch (error) {
	          const message = typeof formatError === "function" ? formatError(error) : (error && error.message) || errorMessage || "Әрекет орындалмады";
	          toast(message, "error");
	          cancelBtn.disabled = false;
	          setButtonLoading(confirmBtn, false);
	        }
	      });
	      backdrop.appendChild(container);
	      backdrop.addEventListener("click", (event) => {
	        if (event.target === backdrop && !buttonIsLoading(confirmBtn)) close(false);
	      });
	      els.modalRoot.appendChild(backdrop);
	    });
	  }

  function openModalShell(title, body) {
    const backdrop = document.createElement("div");
    backdrop.className = "modal-backdrop";
    const container = document.createElement("div");
    container.className = "modal modal-wide";
    container.innerHTML = `
      <div class="modal-head">
        <h2>${esc(title || "")}</h2>
      </div>
      <div class="modal-body">${body || ""}</div>
    `;
    backdrop.appendChild(container);
    backdrop.addEventListener("click", (event) => {
      if (event.target === backdrop && backdrop.dataset.busy !== "1") backdrop.remove();
    });
    els.modalRoot.appendChild(backdrop);
    return {
      backdrop,
      body: container.querySelector(".modal-body"),
      close: () => backdrop.remove(),
    };
  }

  function isLegalAgreementRequired(error) {
    return Boolean(error && error.body && error.body.error === "LEGAL_AGREEMENT_REQUIRED");
  }

  async function runAfterLegalAgreement(action) {
    const accepted = await ensureLegalAgreementAccepted();
    if (!accepted) return null;
    try {
      return await action();
    } catch (error) {
      if (!isLegalAgreementRequired(error)) throw error;
      const acceptedAfterRetry = await openLegalAgreementModal(error.body || {});
      if (!acceptedAfterRetry) return null;
      return action();
    }
  }

  async function ensureLegalAgreementAccepted() {
    const status = await api("/api/legal/agreement-status");
    state.legalAgreementStatus = status;
    if (status && status.accepted) return true;
    return openLegalAgreementModal(status || {});
  }

  function openLegalAgreementModal(meta) {
    return new Promise((resolve) => {
      let currentLanguage = (state.me && state.me.user && state.me.user.language) === "ru" ? "ru" : "kk";
      let currentDocument = null;
      let closed = false;

      const backdrop = document.createElement("div");
      backdrop.className = "modal-backdrop legal-backdrop";
      const container = document.createElement("div");
      container.className = "modal modal-wide legal-modal";
      container.innerHTML = `
        <div class="modal-head legal-modal-head">
          <div>
            <p class="eyebrow">Келісім</p>
            <h2>Құпиялық саясаты және оферта / Политика конфиденциальности и оферта</h2>
          </div>
          <button class="ghost-btn legal-close-btn" data-legal-close type="button" aria-label="Жабу / Закрыть">×</button>
        </div>
        <div class="modal-body legal-modal-body">
          <div class="legal-language-tabs" role="tablist" aria-label="Document language">
            <button class="legal-tab" data-legal-lang="kk" type="button">Қазақша</button>
            <button class="legal-tab" data-legal-lang="ru" type="button">Русский</button>
          </div>
          <div class="legal-doc-shell" data-legal-doc>
            <div class="legal-loading">Құжат жүктелуде...</div>
          </div>
        </div>
        <div class="modal-foot legal-modal-foot">
          <p class="legal-confirm-text" data-legal-confirm-text>Құжатты оқып, шарттармен келісемін</p>
          <button class="ghost-btn" data-legal-close type="button">Жабу / Закрыть</button>
          <button class="gold-btn" data-legal-accept type="button"><span class="btn-label">Келісемін</span><span class="btn-spinner"></span></button>
        </div>
      `;
      backdrop.appendChild(container);
      els.modalRoot.appendChild(backdrop);

      const docNode = container.querySelector("[data-legal-doc]");
      const acceptBtn = container.querySelector("[data-legal-accept]");
      const confirmText = container.querySelector("[data-legal-confirm-text]");
      const close = (value) => {
        if (closed) return;
        closed = true;
        backdrop.remove();
        resolve(value);
      };
      const setBusy = (busy) => {
        backdrop.dataset.busy = busy ? "1" : "";
        acceptBtn.disabled = Boolean(busy);
        container.querySelectorAll("[data-legal-close], [data-legal-lang]").forEach((button) => {
          button.disabled = Boolean(busy);
        });
      };
      const updateCopy = () => {
        acceptBtn.querySelector(".btn-label").textContent = currentLanguage === "ru" ? "Согласен" : "Келісемін";
        confirmText.textContent = currentLanguage === "ru" ? "Я прочитал документ и принимаю условия" : "Құжатты оқып, шарттармен келісемін";
      };
      const updateTabs = () => {
        container.querySelectorAll("[data-legal-lang]").forEach((button) => {
          button.classList.toggle("active", button.dataset.legalLang === currentLanguage);
        });
      };
      const loadDocument = async () => {
        currentDocument = null;
        updateTabs();
        updateCopy();
        acceptBtn.disabled = true;
        docNode.innerHTML = `<div class="legal-loading">${currentLanguage === "ru" ? "Документ загружается..." : "Құжат жүктелуде..."}</div>`;
        try {
          const document = await api(`/api/legal/document?lang=${currentLanguage}`);
          currentDocument = document;
          docNode.innerHTML = `
            <article class="legal-doc-content">
              <h3>${esc(document.title || "")}</h3>
              ${document.content_html || ""}
            </article>
          `;
          acceptBtn.disabled = false;
        } catch (error) {
          docNode.innerHTML = `<div class="form-error">${esc(error.message || "Құжатты жүктеу мүмкін болмады")}</div>`;
        }
      };

      container.querySelectorAll("[data-legal-close]").forEach((button) => button.addEventListener("click", () => close(false)));
      container.querySelectorAll("[data-legal-lang]").forEach((button) => {
        button.addEventListener("click", () => {
          if (button.dataset.legalLang === currentLanguage || backdrop.dataset.busy === "1") return;
          currentLanguage = button.dataset.legalLang;
          loadDocument();
        });
      });
      acceptBtn.addEventListener("click", async () => {
        if (!currentDocument || buttonIsLoading(acceptBtn)) return;
        setButtonLoading(acceptBtn, true);
        setBusy(true);
        try {
          const accepted = await api("/api/legal/accept", {
            method: "POST",
            body: JSON.stringify({
              language: currentLanguage,
              document_type: currentDocument.document_type || meta.document_type || "privacy_policy_offer",
              document_version: currentDocument.document_version || meta.document_version,
            }),
          });
          state.legalAgreementStatus = accepted;
          toast(currentLanguage === "ru" ? "Согласие сохранено" : "Келісім сақталды", "success");
          close(true);
        } catch (error) {
          toast(error.message || (currentLanguage === "ru" ? "Не удалось сохранить согласие" : "Келісімді сақтау мүмкін болмады"), "error");
          setBusy(false);
          setButtonLoading(acceptBtn, false);
        }
      });
      backdrop.addEventListener("click", (event) => {
        if (event.target === backdrop && backdrop.dataset.busy !== "1") close(false);
      });
      loadDocument();
    });
  }

  /* ===========================================================
     BOOT / ROUTING
     =========================================================== */

  async function boot() {
    state.bootedAt = Date.now();
    cacheElements();

    const path = window.location.pathname || "/";
    const isAdminPath = path === "/admin" || path.startsWith("/admin/");

    setupGlobalUi();

    if (isAdminPath) {
      document.documentElement.classList.add("browser-admin-mode");
      document.body.classList.add("browser-admin-mode");
      state.mode = "admin";
      bootBrowserAdmin();
      return;
    }

    const initialTelegram = getTelegram();
    if (isTelegramMiniAppLaunch()) {
      enterMiniAppMode();
      bootMiniApp();
      return;
    }

    if (isDevMiniAppLaunch()) {
      enterMiniAppMode();
      document.documentElement.classList.add("dev-miniapp-mode");
      document.body.classList.add("dev-miniapp-mode");
      updateViewportVars();
      bootMiniApp();
      return;
    }

    if (initialTelegram) {
      enterMiniAppMode();
      showMiniApp();
      renderHeader();
      renderShellLoading();
    }

    const telegramLaunch = await waitForTelegramInitData(TELEGRAM_INIT_WAIT_MS);
    if (telegramLaunch.hasInitData) {
      enterMiniAppMode();
      bootMiniApp(telegramLaunch);
      return;
    }
    if (telegramLaunch.sawWebApp) {
      enterMiniAppMode();
      showMiniApp();
      renderHeader();
      renderError(TELEGRAM_AUTH_FAILED_MESSAGE);
      return;
    }

    document.documentElement.classList.add("landing-mode");
    document.body.classList.add("landing-mode");
    state.mode = "landing";
    renderLanding();
  }

  function enterMiniAppMode() {
    document.documentElement.classList.add("telegram-miniapp-mode");
    document.body.classList.add("telegram-miniapp-mode");
    state.mode = "miniapp";
    if (!state.telegramViewportSetup) {
      setupTelegramViewport();
      state.telegramViewportSetup = true;
    } else {
      syncTelegramFrame();
    }
  }

  function setupGlobalUi() {
    if (els.profileAction) {
      els.profileAction.addEventListener("click", () => {
        if (state.mode === "miniapp") setScreen("profile");
      });
    }
    if (els.adminLogout) els.adminLogout.addEventListener("click", handleAdminLogout);
    if (els.adminLogoutTop) els.adminLogoutTop.addEventListener("click", handleAdminLogout);
    if (els.adminNavToggle) {
      els.adminNavToggle.addEventListener("click", () => {
        if (els.adminApp) els.adminApp.classList.toggle("nav-open");
      });
    }
    if (els.togglePassword && els.adminPassword) {
      els.togglePassword.addEventListener("click", () => {
        const isPwd = els.adminPassword.type === "password";
        els.adminPassword.type = isPwd ? "text" : "password";
        els.togglePassword.textContent = isPwd ? "Жасыру" : "Көрсету";
      });
    }
  }

  /* ===========================================================
     LANDING
     =========================================================== */

  function renderLanding() {
    if (!els.landing) return;
    els.landing.classList.remove("hidden");
    els.landing.setAttribute("aria-hidden", "false");
    if (els.landingTelegramLink) {
      els.landingTelegramLink.href = DEFAULT_CHANNEL_LINK;
    }
  }

  /* ===========================================================
     MINI APP BOOT
     =========================================================== */

  function showMiniApp() {
    els.miniApp.classList.remove("hidden");
    els.miniApp.setAttribute("aria-hidden", "false");
  }

  async function bootMiniApp(telegramLaunch) {
    showMiniApp();
    renderHeader();
    renderShellLoading();

    if (!isDevMiniAppLaunch()) {
      let launch = telegramLaunch;
      if (!hasTelegramInitData()) {
        launch = await waitForTelegramInitData(TELEGRAM_INIT_WAIT_MS);
      }
      if (!hasTelegramInitData()) {
        renderError(launch && launch.sawWebApp ? TELEGRAM_AUTH_FAILED_MESSAGE : TELEGRAM_AUTH_REQUIRED_MESSAGE);
        return;
      }
    }

    try {
      await loadMiniAppData();
    } catch (error) {
      if (error.status === 401 && !isDevMiniAppLaunch()) {
        const retryLaunch = await waitForTelegramInitData(TELEGRAM_INIT_RETRY_MS);
        if (retryLaunch.hasInitData) {
          try {
            await loadMiniAppData();
            return;
          } catch (retryError) {
            error = retryError;
          }
        }
      }
      renderError(
        error.status === 401
          ? TELEGRAM_AUTH_FAILED_MESSAGE
          : error.message || "Mini App-ты жүктеу мүмкін болмады.",
      );
    }
  }

  async function loadMiniAppData() {
    const [me, platform, levels, books, freeLessons, premiumCourses] = await Promise.all([
      api("/api/me"),
      api("/api/platform").catch(() => null),
      api("/api/levels").catch(() => ({ levels: [] })),
      api("/api/books").catch(() => ({ books: [], whatsapp_sales_phone: "" })),
      api("/api/free-lessons").catch(() => ({ free_lessons: [] })),
      api("/api/premium-courses").catch(() => ({ premium_courses: [], whatsapp_sales_phone: "" })),
    ]);
    state.me = me;
    state.platform = platform;
    state.levels = (levels && levels.levels) || [];
    state.books = (books && books.books) || [];
    state.freeLessons = (freeLessons && freeLessons.free_lessons) || [];
    state.premiumCourses = (premiumCourses && premiumCourses.premium_courses) || [];
    state.whatsappSalesPhone = compact((premiumCourses && premiumCourses.whatsapp_sales_phone) || (books && books.whatsapp_sales_phone));

    const user = me && me.user ? me.user : {};
    const sub = user.subscription || {};
    const hasActiveSub = sub && sub.status === "active";
    state.currentScreen =
      user.current_level && user.current_level > 0 ? "dashboard" : hasActiveSub ? "dashboard" : "onboarding";

    renderHeader();
    renderMini();
  }

  /* ===========================================================
     MINI APP NAVIGATION
     =========================================================== */

	  function setScreen(screen) {
	    closeCertificatePopover();
	    state.currentScreen = screen;
	    syncTelegramBackButton();
	    renderMini();
	    if (els.appContent) els.appContent.scrollTo({ top: 0, behavior: "smooth" });
	  }

	  function renderMini() {
	    renderFooter();
	    syncTelegramBackButton();
	    const map = {
      onboarding: renderOnboarding,
      dashboard: renderDashboard,
      diagnostics: renderDiagnostics,
      tariffs: renderTariffs,
      payment: renderPayment,
      premiumCourses: renderPremiumCourses,
      premiumCourseDetail: renderPremiumCourseDetail,
      premiumPayment: renderPremiumPayment,
      financialIq: renderFinancialIq,
	      financialIqResult: renderFinancialIqResult,
	      levels: renderLevels,
	      lessons: renderLessons,
	      lessonDetail: renderLessonDetail,
	      test: renderTest,
      assignment: renderAssignment,
      referral: renderReferral,
      coins: renderCoins,
      streams: renderStreams,
      channels: renderChannels,
      freeLessons: renderFreeLessons,
      freeLessonDetail: renderFreeLessonDetail,
      bookDetail: renderBookDetail,
      profile: renderProfile,
      support: renderSupport,
    };
    const renderer = map[state.currentScreen] || renderDashboard;
    Promise.resolve()
      .then(() => renderer())
	      .catch((error) => renderError(error.message || "Қате орын алды"));
	  }

	  function handleMiniAppBack() {
	    if (state.currentScreen === "payment") {
	      setScreen("tariffs");
	      return;
	    }
	    if (state.currentScreen === "premiumPayment") {
	      setScreen("premiumCourseDetail");
	      return;
	    }
	    if (state.currentScreen === "premiumCourseDetail") {
	      closePremiumCourseDetail();
	      return;
	    }
	    if (state.currentScreen === "premiumCourses") {
	      closePremiumCourses();
	      return;
	    }
	    if (state.currentScreen === "bookDetail") {
	      closeBookDetail();
	      return;
	    }
	    if (state.currentScreen === "freeLessonDetail") {
	      closeFreeLessonDetail();
	      return;
	    }
		    if (state.currentScreen === "freeLessons") {
		      closeFreeLessons();
		      return;
		    }
		    if (state.currentScreen === "lessonDetail") {
		      closeLessonDetail();
		      return;
		    }
		    if (state.currentScreen === "test") {
		      returnFromTest();
		      return;
		    }
		    if (state.currentScreen === "lessons") {
		      setScreen("dashboard");
		      return;
		    }
		    if (state.currentScreen === "financialIq" || state.currentScreen === "financialIqResult") {
		      returnFromFinancialIq();
		    }
		  }

	  function syncTelegramBackButton() {
	    const tg = getTelegram();
	    if (!tg || !tg.BackButton || !telegramVersionAtLeast(tg, "6.1")) return;
	    try {
		      const hasBack = state.currentScreen === "payment" || state.currentScreen === "premiumPayment" || state.currentScreen === "premiumCourses" || state.currentScreen === "premiumCourseDetail" || state.currentScreen === "bookDetail" || state.currentScreen === "freeLessons" || state.currentScreen === "freeLessonDetail" || state.currentScreen === "lessons" || state.currentScreen === "lessonDetail" || state.currentScreen === "test" || state.currentScreen === "financialIq" || state.currentScreen === "financialIqResult";
	      if (hasBack) {
	        if (!state.telegramBackHandlerBound && typeof tg.BackButton.onClick === "function") {
	          tg.BackButton.onClick(handleMiniAppBack);
	          state.telegramBackHandlerBound = true;
	        }
	        if (typeof tg.BackButton.show === "function") tg.BackButton.show();
	      } else if (typeof tg.BackButton.hide === "function") {
	        tg.BackButton.hide();
	      }
	    } catch (_) {}
	  }

  function renderFooter() {
    if (!els.bottomCta) return;
	    const tabs = [
	      ["dashboard", "◉", "Басты"],
	      ["lessons", "▤", "Сабақтар"],
	      ["referral", "↗", "Дос"],
	      ["coins", "✦", "Coin"],
	      ["profile", "◐", "Жеке"],
	    ];
    els.bottomCta.classList.remove("hidden");
    els.bottomCta.innerHTML = `<div class="tabbar">${tabs
      .map(
        ([screen, icon, label]) => `
        <button class="tab-btn ${state.currentScreen === screen ? "active" : ""}" data-screen="${screen}" type="button">
          <span class="tab-icon">${icon}</span>
          <span>${label}</span>
        </button>`,
      )
      .join("")}</div>`;
    els.bottomCta.querySelectorAll("[data-screen]").forEach((button) => {
      button.addEventListener("click", () => setScreen(button.dataset.screen));
    });
  }

  const financialIqQuestions = [
    {
      id: "q1",
      type: "single",
      title: "Менің ай сайынғы кірісім",
      options: [
        ["0-ден 50 000 тг дейін", 0],
        ["50 000-нан 100 000 тг дейін", 1],
        ["100 000-нан 250 000 тг дейін", 2],
        ["250 000-нан 500 000 тг дейін", 4],
        ["500 000-нан 1 000 000 тг дейін", 8],
        ["1 000 000-нан 2 000 000 тг дейін", 12],
        ["2 000 000-нан 4 000 000 тг дейін", 17],
        ["4 000 000 тг-ден бастап", 24],
      ],
    },
    { id: "q2", type: "checkbox", title: "Жалдамалы жұмысшымын", score: 3 },
    { id: "q3", type: "checkbox", title: "Басшылық лауазымдамын", score: 4 },
    { id: "q4", type: "checkbox", title: "Өзіме өзім жұмыс жасаймын (қосымша табыс табу, фриланс)", score: 4 },
    {
      id: "q5",
      type: "single",
      title: "Өзімнің бизнесім бар",
      options: [
        ["1-10 адам", 6],
        ["11-50 адам", 8],
        ["51-100 адам", 12],
        ["100-ден аса адам", 16],
        ["Бизнесім жоқ", 0],
      ],
    },
    { id: "q6", type: "checkbox", title: "Менде салған инвестициямнан түсіп тұратын кірісім бар", score: 2 },
    { id: "q7", type: "checkbox", title: "Әлеуметтік төлемдер алып отырамын (субсидия, льгота)", score: 2 },
    { id: "q8", type: "checkbox", title: "Менің шығындарым кірісімнен асып кетеді", score: -5 },
    { id: "q9", type: "checkbox", title: "Менің шығындарым кірісіммен шамамен тең болып қалады", score: 0 },
    { id: "q10", type: "checkbox", title: "Маған ірі сатып алуларға және демалуға әрқашанда ақша жеткілікті", score: 4 },
    {
      id: "q11",
      type: "single",
      title: "Мен кірісімнің қанша пайызын жинап отырамын?",
      options: [
        ["Шамамен 5%", 1],
        ["10%", 3],
        ["20%", 7],
        ["30%", 10],
        ["30%-дан көп", 16],
        ["Жинамаймын", 0],
      ],
    },
    { id: "q12", type: "checkbox", title: "Мен ақшаны тиын салғышқа жинаймын", score: 0 },
    {
      id: "q13",
      type: "single",
      title: "Менің жинаған ақшам",
      options: [
        ["Бір айлық кіріс сомасынан аз", 1],
        ["Шамамен 1-2 айлық кіріс мөлшері", 3],
        ["Шамамен 3-5 айлық кіріс мөлшері", 6],
        ["6 айлық кіріс мөлшерінен көп", 12],
        ["Жинаған ақшам жоқ", 0],
      ],
    },
    {
      id: "q14",
      type: "single",
      title: "Мен ақшамды банкке жинаймын",
      options: [
        ["Жай ғана банкте сақтаймын", 2],
        ["12% жылдық", 4],
        ["20-30% жылдық", 8],
        ["50% жылдық", 16],
        ["100% жылдық", 25],
        ["Банкке жинамаймын", 0],
      ],
    },
    { id: "q15", type: "checkbox", title: "Менің пассивті кірістерімнің сомасы жалпы шығындарымның сомасынан асып түседі", score: 25 },
    { id: "q16", type: "checkbox", title: "Менің жалға беріп отырған жылжымайтын мүлкім бар", score: 6 },
    { id: "q17", type: "checkbox", title: "Басымда кредит бар", score: -4 },
    { id: "q18", type: "checkbox", title: "Активті кредит картам бар", score: -4 },
    { id: "q19", type: "checkbox", title: "Жеке адамдардан пайызға алған заемдарым бар", score: -8 },
    { id: "q20", type: "checkbox", title: "Пайызсыз қарызым бар", score: -1 },
    { id: "q21", type: "checkbox", title: "Ипотекалық кредитім бар", score: -4 },
    { id: "q22", type: "checkbox", title: "Қайырымдылықпен айналысамын", score: 2 },
    { id: "q23", type: "checkbox", title: "Қатаң түрде кірісім мен шығынымның қаржылық есебін жүргізіп отырамын", score: 4 },
    { id: "q24", type: "checkbox", title: "Алдын-ала 1 айға бюджетім қарастырылған", score: 2 },
    { id: "q25", type: "checkbox", title: "Алдын-ала 1 жылға бюджетім қарастырылған", score: 5 },
    { id: "q26", type: "checkbox", title: "Алдын-ала 3-10 жылға қаржылық жоспарым бар", score: 8 },
    { id: "q27", type: "checkbox", title: "Менің бағасы мен мерзімдері жазылған қаржылық мақсаттарымның тізімі бар", score: 2 },
    { id: "q28", type: "checkbox", title: "Менің жоспарларым мен бюджеттерімде инвестициялар ескерілген", score: 5 },
    { id: "q29", type: "checkbox", title: "Өмірімде ақша өте маңызды тақырып деп санаймын", score: 5 },
    { id: "q30", type: "checkbox", title: "Жеке қаржы бойынша кітаптар оқимын", score: 2 },
    { id: "q31", type: "checkbox", title: "Мен жеке қаржыға байланысты тегін семинарларға барамын", score: 4 },
    { id: "q32", type: "checkbox", title: "Мен жеке қаржыға байланысты ақылы тренингтерге барамын", score: 15 },
    { id: "q33", type: "checkbox", title: "«Денежный поток» немесе «Монополия» ойындарын ойнап көрдім", score: 3 },
  ];

  const financialIqRanges = [
    {
      min: -Infinity,
      max: 40,
      title: "0-40 балл аралығы",
      level: "Қаржылық IQ деңгейі — өте төмен",
      text: "Сіз ақшаны емес, сізді ақша басқарады. Ақшаңызды басқару қиынға соғып жүр немесе ақшаны басқаруды маңызды деп санамайсыз.\n\nӘр түрлі кезең сайын келіп тұратын және қайталана беретін қаржылық проблемаларыңыз бар. Егер ақшаны басқаруға көңіл бөлмесеңіз, арты жақсы болмайтын жағдайлар болуы мүмкін.",
    },
    {
      min: 41,
      max: 80,
      title: "41-80 балл аралығы",
      level: "Қаржылық IQ деңгейі — орташа",
      text: "Сіз қаржылық тапсырмалармен айналысуды бастап келе жатырсыз. Ақшаңызды басқаруға мүмкіндігіңіз бар, сіз кірісіңіз бен шығысыңызға әсер ете аласыз.\n\nСізде жаңа қаржылық жетістіктерге қызығушылық бар және оларды пайдалануға дайынсыз.\n\nЕгер қосымша білім алып, әрекет ететін болсаңыз, қаржылық деңгейдің келесі сатысына тез көтерілуге мүмкіндік бар.",
    },
    {
      min: 81,
      max: 140,
      title: "81-140 балл аралығы",
      level: "Қаржылық IQ деңгейі — жоғары",
      text: "Сіз сенімді түрде ақшаңызды басқарып жүрсіз. Ақша заңдылығын жақсы түсінесіз және өз пайдаңызға қарай қолдана аласыз.\n\nСізде анықталған қаржылық стратегия бар, жақын арада қаржылық өсу күтіледі. Сізге осы бағытыңызды жүйелендіру керек, сонда қаржылық еркіндік пен тәуелсіздікке жақындайсыз.",
    },
    {
      min: 141,
      max: Infinity,
      title: "141-200 балл аралығы",
      level: "Қаржылық IQ деңгейі — өте жоғары",
      text: "Сізді бай адам деп айтуға болады. Құттықтаймыз!\n\nАқша сізге жұмыс жасап жатыр және сізге қуаныш сыйлайды. Әдетте сіздің кірісіңіз шығыныңыздан әлдеқайда жоғары. Енді инвестициялық портфеліңізді сауатты түрде реттеп, тұрақты өсуді жүйелеу маңызды.",
    },
  ];

	  function financialIqCtaCard() {
	    const saved = savedFinancialIqResult();
	    if (saved) {
	      return `<article class="card financial-iq-card compact-result">
	        <div>
	          <p class="eyebrow">Қаржылық IQ</p>
	          <h2>${esc(saved.result_level || "Нәтиже сақталды")}</h2>
	          <p class="muted">Сіз қаржылық IQ тестін аяқтадыңыз. Нәтижеңіз: ${esc(saved.score)} балл</p>
	        </div>
	        <button class="ghost-btn" data-financial-iq type="button">Қайта тапсыру</button>
	      </article>`;
	    }
	    return `<article class="card financial-iq-card">
	      <div>
	        <p class="eyebrow">Тест</p>
	        <h2>Қаржылық IQ тесті</h2>
	        <p class="muted">33 сұраққа жауап беріп, қаржылық деңгейіңізді анықтаңыз.</p>
	      </div>
	      <button class="gold-btn" data-financial-iq type="button">Тесттен өту</button>
	    </article>`;
	  }

	  function savedFinancialIqResult() {
	    return (state.me && state.me.financial_iq) || null;
	  }

	  function financialIqResultForView(result) {
	    if (!result) return null;
	    if (result.result_title || result.result_level) {
	      return {
	        score: result.score,
	        title: result.result_title || "",
	        level: result.result_level || "",
	        text: result.result_text || "",
	      };
	    }
	    return result;
	  }

	  function personalDashboardCard(user, progress, sub, percent) {
	    const iq = savedFinancialIqResult();
	    const subTitle = sub && sub.tariff_code ? sub.tariff_code : "Жоқ";
	    return `<article class="card dashboard-card progress-dashboard-card">
	      <div class="card-header">
	        <div>
	          <p class="eyebrow">Жеке прогресс</p>
	          <h2>Менің көрсеткіштерім</h2>
	        </div>
	        <span class="pill">Деңгей ${esc(user.current_level || 0)}</span>
	      </div>
	      <div class="dashboard-mini-grid progress-stats-grid">
	        <div class="progress-stat-card">
	          <span class="muted small">Қаржылық IQ</span>
	          <strong>${iq ? `${esc(iq.score)} балл` : "—"}</strong>
	          <p class="muted">${esc(iq ? iq.result_level : "Тесттен өтіп, нәтижеңізді сақтаңыз.")}</p>
	        </div>
	        <div class="progress-stat-card">
	          <span class="muted small">Прогресс</span>
	          <strong>${esc(percent)}%</strong>
	          <p class="muted">${esc(progress.next_requirement || "Келесі қадам дайындалады.")}</p>
	        </div>
	        <div class="progress-stat-card">
	          <span class="muted small">Тариф</span>
	          <strong>${esc(subTitle)}</strong>
	          <p class="muted">${esc(sub && sub.expires_at ? formatDate(sub.expires_at) : "Белсенді жазылым жоқ")}</p>
	        </div>
	      </div>
	      <button class="${iq ? "ghost-btn" : "gold-btn"} progress-iq-action" data-financial-iq type="button">${iq ? "Қайта тапсыру" : "Қаржылық IQ тестін тапсыру"}</button>
	    </article>`;
	  }

	  function bindFinancialIqCta() {
	    document.querySelectorAll("[data-financial-iq]").forEach((button) => {
	      button.addEventListener("click", openFinancialIq);
	    });
	  }

	  function bindCertificateGoal() {
	    const button = document.getElementById("certificateGoal");
	    if (!button) return;
	    button.addEventListener("click", (event) => {
	      event.stopPropagation();
	      toggleCertificatePopover(button);
	    });
	  }

	  function toggleCertificatePopover(anchor) {
	    if (certificatePopoverNode) {
	      closeCertificatePopover();
	      if (anchor) anchor.setAttribute("aria-expanded", "false");
	      return;
	    }
	    if (!anchor || !els.modalRoot) return;
	    const popover = document.createElement("div");
	    popover.className = "certificate-popover";
	    popover.innerHTML = `
	      <button class="certificate-popover-close" type="button" aria-label="Жабу">×</button>
	      <p>Курсты толық аяқтаған соң сертификат/диплом аласыз.</p>
	    `;
	    els.modalRoot.appendChild(popover);
	    const rect = anchor.getBoundingClientRect();
	    const width = Math.min(240, Math.max(180, window.innerWidth - 24));
	    const left = Math.max(12, Math.min(window.innerWidth - width - 12, rect.right - width));
	    let top = rect.bottom + 10;
	    if (top + 110 > window.innerHeight) top = Math.max(12, rect.top - 104);
	    popover.style.width = `${width}px`;
	    popover.style.left = `${left}px`;
	    popover.style.top = `${top}px`;
	    certificatePopoverNode = popover;
	    anchor.setAttribute("aria-expanded", "true");
	    popover.querySelector(".certificate-popover-close").addEventListener("click", closeCertificatePopover);
	    certificatePopoverOutsideHandler = (event) => {
	      if (!certificatePopoverNode) return;
	      if (certificatePopoverNode.contains(event.target) || anchor.contains(event.target)) return;
	      closeCertificatePopover();
	    };
	    window.setTimeout(() => document.addEventListener("click", certificatePopoverOutsideHandler), 0);
	  }

	  function closeCertificatePopover() {
	    if (certificatePopoverOutsideHandler) {
	      document.removeEventListener("click", certificatePopoverOutsideHandler);
	      certificatePopoverOutsideHandler = null;
	    }
	    if (certificatePopoverNode) {
	      certificatePopoverNode.remove();
	      certificatePopoverNode = null;
	    }
	    const button = document.getElementById("certificateGoal");
	    if (button) button.setAttribute("aria-expanded", "false");
	  }

	  function openFinancialIq() {
	    if (state.currentScreen !== "financialIq" && state.currentScreen !== "financialIqResult") {
	      state.financialIqReturnScreen = state.currentScreen || "dashboard";
	    }
	    state.financialIqAnswers = {};
	    state.financialIqResult = null;
	    setScreen("financialIq");
	  }

	  function returnFromFinancialIq() {
	    const target = state.financialIqReturnScreen || "dashboard";
	    setScreen(target === "financialIq" || target === "financialIqResult" ? "dashboard" : target);
	  }

	  function financialIqResultForScore(score) {
	    const range = financialIqRanges.find((item) => score >= item.min && score <= item.max) || financialIqRanges[financialIqRanges.length - 1];
	    return Object.assign({ score }, range);
	  }

	  function financialIqQuestionHtml(question, index) {
	    if (question.type === "single") {
	      return `<fieldset class="card iq-question">
	        <div class="iq-question-head">
	          <span class="iq-question-number">${index + 1}</span>
	          <h3>${esc(question.title)}</h3>
	        </div>
	        <div class="iq-options">
	          ${(question.options || [])
	            .map(
	              ([label], optionIndex) => `<label class="test-option">
	                <input name="${esc(question.id)}" value="${esc(optionIndex)}" type="radio" />
	                <span>${esc(label)}</span>
	              </label>`,
	            )
	            .join("")}
	        </div>
	      </fieldset>`;
	    }
	    return `<fieldset class="card iq-question compact">
	      <label class="test-option">
	        <input name="${esc(question.id)}" type="checkbox" />
	        <span><strong>${index + 1}.</strong> ${esc(question.title)}</span>
	      </label>
	    </fieldset>`;
	  }

	  function calculateFinancialIq(form) {
	    const fd = new FormData(form);
	    const answers = {};
	    let score = 0;
	    financialIqQuestions.forEach((question) => {
	      if (question.type === "single") {
	        const raw = fd.get(question.id);
	        answers[question.id] = raw == null ? "" : String(raw);
	        const option = question.options && question.options[Number(raw)];
	        if (option) score += Number(option[1]) || 0;
	      } else {
	        const checked = fd.get(question.id) === "on";
	        answers[question.id] = checked;
	        if (checked) score += Number(question.score) || 0;
	      }
	    });
	    state.financialIqAnswers = answers;
	    state.financialIqResult = financialIqResultForScore(score);
	  }

  /* ===========================================================
     MINI APP SCREENS
     =========================================================== */

  function renderShellLoading() {
    html(`
      <section class="screen">
        <div class="hero skeleton" style="min-height:220px"></div>
        <div class="grid three">
          <div class="metric skeleton"></div>
          <div class="metric skeleton"></div>
          <div class="metric skeleton"></div>
        </div>
        <div class="card skeleton" style="min-height:140px"></div>
      </section>
    `);
  }

  function renderError(message) {
    html(`
      <section class="screen">
        <div class="error-card">
          <p class="eyebrow">Қате</p>
          <h2>Жүктеу мүмкін болмады</h2>
          <p>${esc(message || "Белгісіз қате")}</p>
          <button class="ghost-btn" id="retryBoot" type="button">Қайталау</button>
        </div>
      </section>
    `);
    on("retryBoot", bootMiniApp);
  }

  function renderOnboarding() {
    html(`
      <section class="screen">
        <div class="hero">
	          <p class="eyebrow">ZHENIS ORDA UNIVERSE</p>
	          <h1>Қош келдіңіз! Сіз жай курсқа емес, жүйелі даму ортасына кірдіңіз.</h1>
	          <p class="muted">Ойлау, қаржы, бизнес және лидерлік бойынша саты-саты дамуға арналған платформа.</p>
          <div class="pill-row">
            <span class="pill">Мәртебе</span>
            <span class="pill">Мақтаныш</span>
            <span class="pill">Мотивация</span>
          </div>
        </div>
        <div class="grid two">
          <button class="gold-btn lg" id="goDiagnostics" type="button">Тегін диагностика</button>
          <button class="ghost-btn lg" id="goTariffs" type="button">Тариф таңдау</button>
        </div>
        ${savedFinancialIqResult() ? "" : financialIqCtaCard()}
        ${premiumCoursesHomeSection()}
        ${freeLessonsHomeSection()}
        ${booksHomeSection()}
        <div class="card">
          <p class="eyebrow">Premium жабық клуб</p>
          <h2>Жабық мүшелік</h2>
          <p class="muted">Тек тариф ашқан клиенттер ғана сабақтарға, тестке, эфирге және жеке арналарға қол жеткізе алады.</p>
        </div>
      </section>
    `);
    on("goDiagnostics", () => setScreen("diagnostics"));
    on("goTariffs", () => setScreen("tariffs"));
    bindNext();
    bindFinancialIqCta();
    bindFreeLessonCards();
    bindPremiumCourseCards();
    bindBookCards();
  }

  function renderDashboard() {
    const user = (state.me && state.me.user) || {};
    const progress = (state.me && state.me.progress) || {};
    const sub = user.subscription || {};
    const subStatus = sub.status || "inactive";
    const subOk = subStatus === "active";
    const currentLevel = user.current_level || 0;
    const percent = Math.max(0, Math.min(100, num(progress.percent)));

    html(`
      <section class="screen">
        <div class="hero">
	          <p class="eyebrow">Жүйелі даму платформасы</p>
	          <h1>ZHENIS ORDA UNIVERSE</h1>
	          <p class="muted">${esc(progress.next_requirement || "Ойлау, қаржы, бизнес және лидерлік бойынша жүйелі дамыңыз.")}</p>
	          <div class="progress-wrap">
	            <div class="progress-track"><div class="progress-fill" style="--progress:${percent}%"></div></div>
	            <button class="certificate-goal ${percent >= 100 ? "active" : ""}" id="certificateGoal" type="button" aria-label="Сертификат" aria-expanded="false"><span class="certificate-icon" aria-hidden="true"></span></button>
	          </div>
	          <div class="progress-row">
	            <span>${percent >= 100 ? "Сертификат" : "Сіздің прогресіңіз"}</span>
	            <strong>${percent}%</strong>
	          </div>
        </div>

        <div class="grid three">
          ${metric("Тариф", sub.tariff_code || "Жоқ", statusText[subStatus] || subStatus, subOk ? "ok" : subStatus === "expired" ? "bad" : "warn")}
	          ${metric("Деңгей", `Деңгей ${currentLevel}`, currentLevel ? "Ашық" : "Жабық", currentLevel ? "ok" : "warn")}
          ${metric("ZHENIS Coin", money(user.coin_balance), "ішкі валюта")}
        </div>

        ${personalDashboardCard(user, progress, sub, percent)}

        ${savedFinancialIqResult() ? "" : financialIqCtaCard()}

        ${freeLessonsHomeSection()}

        ${premiumCoursesHomeSection()}

        ${booksHomeSection()}

        <div class="card">
          <div class="card-header">
            <div>
              <p class="eyebrow">Келесі қадам</p>
              <h2>Келесі талап</h2>
            </div>
            <span class="status ${progress.can_unlock_next ? "ok" : "warn"}">${progress.can_unlock_next ? "Дайын" : "Жабық"}</span>
          </div>
	          <p class="muted">${esc(progress.next_requirement || "2-деңгей ашылуы үшін тест тапсырыңыз.")}</p>
          <div class="grid two tight" style="margin-top:6px">
            <button class="gold-btn" data-next="lessons" type="button">Сабақтарға өту</button>
            <button class="ghost-btn" data-next="test" type="button">Тест тапсыру</button>
          </div>
        </div>

        <div class="card">
          <div class="section-head">
	            <h2>12 деңгейлік жол</h2>
            <button class="ghost-btn" data-next="levels" type="button">Барлығы</button>
          </div>
          ${renderLevelStrip()}
        </div>

        <div class="grid two">
          <button class="ghost-btn lg" data-next="tariffs" type="button">Тарифтер</button>
	          <button class="ghost-btn lg" data-next="referral" type="button">Дос шақыру</button>
          <button class="ghost-btn lg" data-next="streams" type="button">Жабық эфир</button>
          <button class="ghost-btn lg" data-next="channels" type="button">Жабық арналар</button>
          <button class="ghost-btn lg" data-next="premiumCourses" type="button">Premium курстар</button>
          <button class="ghost-btn lg" data-next="assignment" type="button">Тапсырмаларым</button>
          <button class="ghost-btn lg" data-next="support" type="button">Қолдау қызметі</button>
          <button class="ghost-btn lg whatsapp-action" id="dashboardWhatsappSupport" type="button">
            <span class="whatsapp-action-mark" aria-hidden="true">
              <svg viewBox="0 0 32 32" width="16" height="16" focusable="false" aria-hidden="true">
                <path fill="currentColor" d="M16.02 5.333c-5.9 0-10.687 4.787-10.687 10.687 0 1.88.493 3.713 1.427 5.333L5.333 26.667l5.46-1.4a10.66 10.66 0 0 0 5.227 1.36h.004c5.899 0 10.687-4.788 10.687-10.687 0-2.854-1.111-5.535-3.129-7.553a10.617 10.617 0 0 0-7.562-3.054Zm0 19.45h-.003a8.86 8.86 0 0 1-4.517-1.237l-.324-.192-3.24.829.866-3.16-.211-.336a8.86 8.86 0 0 1-1.358-4.717c0-4.9 3.987-8.887 8.89-8.887 2.373 0 4.604.926 6.281 2.606a8.829 8.829 0 0 1 2.605 6.288c0 4.9-3.987 8.806-8.989 8.806Zm5.124-6.624c-.281-.141-1.663-.821-1.92-.914-.257-.094-.444-.141-.631.141-.187.281-.722.914-.886 1.101-.164.187-.327.21-.609.07-.281-.14-1.187-.437-2.262-1.395-.835-.745-1.4-1.665-1.564-1.946-.164-.282-.018-.434.124-.574.127-.127.281-.328.422-.492.14-.164.187-.281.281-.469.094-.187.047-.351-.023-.492-.07-.141-.633-1.523-.866-2.084-.227-.547-.46-.472-.633-.481-.164-.008-.351-.01-.539-.01a1.04 1.04 0 0 0-.751.351c-.258.281-.984.961-.984 2.344 0 1.383 1.008 2.719 1.148 2.906.141.187 1.984 3.028 4.805 4.244.671.291 1.196.464 1.604.594.674.214 1.287.184 1.772.112.541-.081 1.663-.679 1.898-1.336.234-.656.234-1.219.164-1.336-.07-.117-.258-.187-.539-.328Z"/>
              </svg>
            </span>
            <span>WhatsApp арқылы жазу</span>
          </button>
        </div>
      </section>
    `);
    bindNext();
    on("dashboardWhatsappSupport", openWhatsAppSupport);
    bindFinancialIqCta();
    bindCertificateGoal();
    bindFreeLessonCards();
    bindPremiumCourseCards();
    bindBookCards();
  }

	  function premiumCoursesHomeSection() {
	    const courses = state.premiumCourses || [];
	    if (!courses.length) return "";
	    return `<section class="premium-courses-section" aria-label="Premium курстар">
	      <div class="section-head">
	        <div>
	          <p class="eyebrow">Premium курстар</p>
	          <h2>Бөлек ақылы курстар</h2>
	        </div>
	        <button class="ghost-btn" data-next="premiumCourses" type="button">Барлығы</button>
	      </div>
	      <div class="premium-course-grid">${courses.map(premiumCourseCard).join("")}</div>
	    </section>`;
	  }

	  function premiumCourseCard(course) {
	    const access = Boolean(course.access);
	    const image = visiblePremiumCourseImage(course);
	    return `<article class="premium-course-card ${access ? "open" : "locked"}" data-premium-course="${esc(course.id)}" tabindex="0" role="button" aria-label="${esc(course.title)}">
	      ${image ? `<img class="premium-course-image" src="${esc(image)}" alt="${esc(course.title)}" loading="lazy" />` : ""}
	      <div class="premium-course-body">
	        <div class="card-header">
	          <div>
	            <p class="eyebrow">${access ? "Ашық" : "Құлыпталған"}</p>
	            <h3>${access ? "✓" : "🔒"} ${esc(course.title)}</h3>
	          </div>
	          <span class="status ${access ? "ok" : "bad"}">${access ? "Ашық" : "Құлыпталған"}</span>
	        </div>
	        <p class="muted">Бағасы: ${money(course.price_kzt)} тг</p>
	        <div class="action-row">
	          <button class="${access ? "ghost-btn" : "gold-btn"}" data-open-premium-course="${esc(course.id)}" type="button">${access ? "Курсты ашу" : "Сатып алу"}</button>
	        </div>
	      </div>
	    </article>`;
	  }

	  function bindPremiumCourseCards() {
	    document.querySelectorAll("[data-premium-course]").forEach((card) => {
	      card.addEventListener("click", (event) => {
	        if (event.target.closest("[data-open-premium-course]")) return;
	        openPremiumCourseDetail(card.dataset.premiumCourse);
	      });
	      card.addEventListener("keydown", (event) => {
	        if (event.key === "Enter" || event.key === " ") {
	          event.preventDefault();
	          openPremiumCourseDetail(card.dataset.premiumCourse);
	        }
	      });
	    });
	    document.querySelectorAll("[data-open-premium-course]").forEach((button) => {
	      button.addEventListener("click", (event) => {
	        event.stopPropagation();
	        openPremiumCourseDetail(button.dataset.openPremiumCourse);
	      });
	    });
	  }

	  function openPremiumCourses() {
	    state.premiumCourseReturnScreen = state.currentScreen && state.currentScreen !== "premiumCourses" ? state.currentScreen : "dashboard";
	    setScreen("premiumCourses");
	  }

	  function closePremiumCourses() {
	    const target = state.premiumCourseReturnScreen || "dashboard";
	    setScreen(target === "premiumCourses" || target === "premiumCourseDetail" || target === "premiumPayment" ? "dashboard" : target);
	  }

	  function openPremiumCourseDetail(courseID) {
	    if (!courseID) return;
	    state.selectedPremiumCourseId = courseID;
	    if (state.currentScreen !== "premiumCourses" && state.currentScreen !== "premiumCourseDetail" && state.currentScreen !== "premiumPayment") {
	      state.premiumCourseReturnScreen = state.currentScreen || "dashboard";
	    }
	    setScreen("premiumCourseDetail");
	  }

	  function closePremiumCourseDetail() {
	    state.selectedPremiumCourseId = null;
	    const target = state.premiumCourseReturnScreen || "dashboard";
	    setScreen(target === "premiumCourseDetail" || target === "premiumPayment" ? "dashboard" : target);
	  }

	  async function renderPremiumCourses() {
	    let courses = state.premiumCourses || [];
	    if (!courses.length) {
	      try {
	        const data = await api("/api/premium-courses");
	        courses = data.premium_courses || [];
	        state.premiumCourses = courses;
	        state.whatsappSalesPhone = compact(data.whatsapp_sales_phone || state.whatsappSalesPhone);
	      } catch (error) {
	        html(`<section class="screen"><button class="ghost-btn mini-back-btn" id="backPremiumCourses" type="button">Артқа</button>${emptyState("Premium курстарды жүктеу мүмкін болмады.")}</section>`);
	        on("backPremiumCourses", closePremiumCourses);
	        return;
	      }
	    }
	    html(`
	      <section class="screen premium-courses-screen">
	        <div class="section-head">
	          <div>
	            <p class="eyebrow">Premium курстар</p>
	            <h1>Premium курстар</h1>
	          </div>
	          <button class="ghost-btn mini-back-btn" id="backPremiumCourses" type="button">Артқа</button>
	        </div>
	        ${courses.length ? `<div class="premium-course-grid large">${courses.map(premiumCourseCard).join("")}</div>` : emptyState("Қазір premium курс жоқ.")}
	      </section>
	    `);
	    on("backPremiumCourses", closePremiumCourses);
	    bindPremiumCourseCards();
	  }

	  async function renderPremiumCourseDetail() {
	    const courseID = state.selectedPremiumCourseId;
	    if (!courseID) {
	      html(`<section class="screen"><button class="ghost-btn mini-back-btn" id="backPremiumCourse" type="button">Артқа</button>${emptyState("Курс табылмады.")}</section>`);
	      on("backPremiumCourse", closePremiumCourseDetail);
	      return;
	    }
	    html(`<section class="screen"><button class="ghost-btn mini-back-btn" id="backPremiumCourse" type="button">Артқа</button><div class="card skeleton"></div><div class="grid">${skeletonRows(3)}</div></section>`);
	    on("backPremiumCourse", closePremiumCourseDetail);
	    let course;
	    try {
	      const data = await api(`/api/premium-courses/${courseID}`);
	      if (state.currentScreen !== "premiumCourseDetail" || state.selectedPremiumCourseId !== courseID) return;
	      course = data.premium_course;
	      state.whatsappSalesPhone = compact(data.whatsapp_sales_phone || state.whatsappSalesPhone);
	      if (course && course.id) {
	        const courses = state.premiumCourses || [];
	        const index = courses.findIndex((item) => item.id === course.id);
	        if (index >= 0) courses[index] = Object.assign({}, courses[index], course);
	        else courses.push(course);
	      }
	    } catch (error) {
	      html(`<section class="screen"><button class="ghost-btn mini-back-btn" id="backPremiumCourse" type="button">Артқа</button>${emptyState(error.message || "Курс табылмады.")}</section>`);
	      on("backPremiumCourse", closePremiumCourseDetail);
	      return;
	    }
	    if (!course) {
	      html(`<section class="screen"><button class="ghost-btn mini-back-btn" id="backPremiumCourse" type="button">Артқа</button>${emptyState("Курс табылмады.")}</section>`);
	      on("backPremiumCourse", closePremiumCourseDetail);
	      return;
	    }
	    const image = visiblePremiumCourseImage(course);
	    const lessons = course.lessons || [];
	    const previewLessons = lessons.filter((lesson) => lesson.is_preview);
	    if (!course.access) {
	      html(`
	        <section class="screen premium-course-detail-screen">
	          <div class="section-head">
	            <button class="ghost-btn mini-back-btn" id="backPremiumCourse" type="button">Артқа</button>
	          </div>
	          <article class="premium-course-hero ${image ? "has-image" : ""}">
	            ${image ? `<img src="${esc(image)}" alt="${esc(course.title)}" loading="lazy" />` : ""}
	            <div>
	              <p class="eyebrow">Құлыпталған</p>
	              <h1>🔒 ${esc(course.title)}</h1>
	              <div class="price">${money(course.price_kzt)} <small>₸</small></div>
	            </div>
	          </article>
	          <div class="card premium-locked-card">
	            <p class="muted">Бұл курс бөлек ақылы өнім. Қолжетімділік алу үшін төлем жасаңыз немесе менеджерге жазыңыз.</p>
	            <div class="grid two tight">
	              <button class="gold-btn" id="buyPremiumCourse" type="button">Курсты сатып алу</button>
	              <button class="ghost-btn" id="contactPremiumManager" type="button">Менеджерге жазу</button>
	              <button class="ghost-btn" id="backPremiumCourse2" type="button">Артқа</button>
	            </div>
	          </div>
	          ${previewLessons.length ? `<div class="section-head"><h2>Preview video</h2></div><div class="grid">${previewLessons.map(premiumLessonCard).join("")}</div>` : ""}
	        </section>
	      `);
	      on("backPremiumCourse", closePremiumCourseDetail);
	      on("backPremiumCourse2", closePremiumCourseDetail);
	      on("buyPremiumCourse", () => setScreen("premiumPayment"));
	      on("contactPremiumManager", () => openExternalLink(premiumCourseManagerUrl(course)));
	      bindPremiumLessonCards();
	      return;
	    }
	    html(`
	      <section class="screen premium-course-detail-screen">
	        <div class="section-head">
	          <button class="ghost-btn mini-back-btn" id="backPremiumCourse" type="button">Артқа</button>
	        </div>
	        <article class="premium-course-hero ${image ? "has-image" : ""}">
	          ${image ? `<img src="${esc(image)}" alt="${esc(course.title)}" loading="lazy" />` : ""}
	          <div>
	            <p class="eyebrow">Ашық</p>
	            <h1>${esc(course.title)}</h1>
	            ${course.description ? `<p class="muted">${esc(course.description)}</p>` : ""}
	          </div>
	        </article>
	        ${course.telegram_configured ? `<button class="gold-btn lg block" id="premiumTelegramInvite" type="button">${esc(course.telegram_button_title || "Telegram каналға кіру")}</button>` : ""}
	        <div class="grid">${lessons.length ? lessons.map(premiumLessonCard).join("") : emptyState("Бұл курсқа сабақтар әлі қосылған жоқ.")}</div>
	      </section>
	    `);
	    on("backPremiumCourse", closePremiumCourseDetail);
	    on("premiumTelegramInvite", () => openPremiumTelegramInvite(course.id));
	    bindPremiumLessonCards();
	  }

	  function premiumLessonCard(lesson) {
	    const access = Boolean(lesson.access);
	    return `<article class="lesson-card ${access ? "" : "locked"}">
	      <div class="card-header">
	        <div>
	          <p class="eyebrow">${lesson.is_preview ? "Preview video" : `Lesson ${esc(lesson.position || "")}`}</p>
	          <h2>${esc(lesson.title)}</h2>
	        </div>
	        <span class="status ${access ? "ok" : "bad"}">${access ? "Ашық" : "Құлыпталған"}</span>
	      </div>
	      <p class="muted">${esc(lesson.description || "")}</p>
	      <button class="${access ? "gold-btn" : "ghost-btn"}" data-premium-lesson="${esc(lesson.id)}" ${access ? "" : "disabled"} type="button">${access ? "Ашу" : "Жабық"}</button>
	    </article>`;
	  }

	  function bindPremiumLessonCards() {
	    document.querySelectorAll("[data-premium-lesson]").forEach((button) => {
	      button.addEventListener("click", () => openPremiumLesson(button.dataset.premiumLesson));
	    });
	  }

	  async function openPremiumLesson(lessonID) {
	    if (!lessonID) return;
	    try {
	      const data = await api(`/api/premium-course-lessons/${lessonID}`);
	      const lesson = data.lesson || {};
	      await modal({
	        title: lesson.title || "Сабақ",
	        body: `
	          ${lesson.video_url ? `<div class="youtube-frame"><iframe src="${esc(lesson.video_url)}" title="${esc(lesson.title || "Premium lesson")}" loading="lazy" referrerpolicy="strict-origin-when-cross-origin" allow="autoplay; encrypted-media; fullscreen"></iframe></div>` : ""}
	          ${lesson.content_text ? `<div class="book-description">${bookParagraphs(lesson.content_text)}</div>` : `<p class="muted">${esc(lesson.description || "Сабақ ашық.")}</p>`}
	        `,
	        actions: [{ label: "Жабу", value: "ok", primary: true }],
	      });
	    } catch (error) {
	      toast(error.message || "Сабақ құлыпталған", "error");
	    }
	  }

	  async function openPremiumTelegramInvite(courseID) {
	    try {
	      const res = await api(`/api/premium-courses/${courseID}/telegram-invite`, {
	        method: "POST",
	        body: "{}",
	      });
	      openTelegramLink(res.invite_link);
	      toast("Telegram шақыру сілтемесі дайын", "success");
	    } catch (error) {
	      toast(error.message || "Telegram сілтемесін алу мүмкін болмады", "error");
	    }
	  }

	  async function renderPremiumPayment() {
	    const course = selectedPremiumCourse();
	    if (!course) {
	      html(`<section class="screen"><button class="ghost-btn mini-back-btn" id="backPremiumPayment" type="button">Артқа</button>${emptyState("Курс табылмады.")}</section>`);
	      on("backPremiumPayment", () => setScreen("premiumCourses"));
	      return;
	    }
	    html(`
	      <section class="screen">
	        <div class="section-head">
	          <button class="ghost-btn mini-back-btn" id="backPremiumPayment" type="button">Артқа</button>
	        </div>
	        <div class="card">
	          <p class="eyebrow">Premium курс төлемі</p>
	          <h1>${esc(course.title)}</h1>
	          <div class="price">${money(course.price_kzt)} <small>₸</small></div>
	          <p class="muted">Бұл төлем жазылымды ұзартпайды. Kaspi арқылы төлем жасағаннан кейін PDF-чекті Telegram ботқа жіберіңіз.</p>
	        </div>
	        <form id="premiumPaymentForm" class="form">
	          ${staticKaspiMethodHtml()}
	          <button class="gold-btn lg" type="submit"><span class="btn-label">Курсты сатып алу</span><span class="btn-spinner"></span></button>
	        </form>
	        <div id="premiumPaymentResult"></div>
	      </section>
	    `);
	    on("backPremiumPayment", () => setScreen("premiumCourseDetail"));
	    document.getElementById("premiumPaymentForm").addEventListener("submit", async (event) => {
	      event.preventDefault();
	      const submitBtn = event.currentTarget.querySelector("button[type=submit]");
	      if (buttonIsLoading(submitBtn)) return;
	      setButtonLoading(submitBtn, true);
	      try {
	        const provider = DEFAULT_PAYMENT_PROVIDER;
	        const contactPhone = await openPurchasePhoneSheet({
	          title: course.title,
	          amount: course.price_kzt,
	        });
	        if (!contactPhone) return;
	        const res = await runAfterLegalAgreement(() =>
	          api(`/api/premium-courses/${course.id}/payments`, {
	            method: "POST",
	            body: JSON.stringify({ provider, contact_phone: contactPhone }),
	          }),
	        );
	        if (!res) return;
	        if (state.me && state.me.user && res.payment && res.payment.contact_phone) {
	          state.me.user.phone = res.payment.contact_phone;
	        }
	        document.getElementById("premiumPaymentResult").innerHTML = `
	          <div class="card">
	            <p class="eyebrow">Төлем құрылды</p>
	            <h2>Төлем күтіліп тұр</h2>
	            <p class="muted">Kaspi арқылы төлем жасап, PDF-чекті Telegram ботқа жіберіңіз.</p>
	            <p>Сома: <strong>${money(res.payment.amount_kzt)} ₸</strong></p>
	          </div>
	          ${receiptUploadHtml(res.payment)}
	        `;
	        bindReceiptUpload(res.payment.id);
	        await openPaymentInstructionSheet(res.payment, res.instructions, provider);
	        toast("Premium курс төлемі құрылды", "success");
	      } catch (error) {
	        toast(error.message || "Төлем жасау мүмкін болмады", "error");
	      } finally {
	        setButtonLoading(submitBtn, false);
	      }
	    });
	  }

	  function freeLessonsHomeSection() {
	    const lessons = state.freeLessons || [];
	    if (!lessons.length) return "";
	    return `<section class="free-lessons-section" aria-label="Тегін сабақтар">
	      <button class="free-lessons-entry" data-free-lessons type="button" aria-label="Тегін сабақтар">
	        <div>
	          <span class="free-lessons-entry-label">АШЫҚ КОНТЕНТ</span>
	          <strong>Тегін сабақтар</strong>
	          <span>Қолжетімді тегін сабақтарды қараңыз</span>
	        </div>
	        <span class="free-lessons-entry-badge">ТЕГІН</span>
	        <span class="free-lessons-entry-cta">Сабақтарды ашу</span>
	      </button>
	    </section>`;
	  }

	  function freeLessonCard(lesson) {
	    const image = visibleFreeLessonImage(lesson);
	    return `<article class="free-lesson-card" data-free-lesson-detail="${esc(lesson.id)}" tabindex="0" role="button" aria-label="${esc(lesson.title)}">
	      <div class="free-lesson-media">
	        ${image ? `<img src="${esc(image)}" alt="${esc(lesson.title)}" loading="lazy" />` : `<div class="free-lesson-placeholder" aria-hidden="true">ZO</div>`}
	        <div class="free-lesson-shade"></div>
	        <span class="free-lesson-badge">Free</span>
	      </div>
	      <div class="free-lesson-card-body">
	        <h3>${esc(lesson.title)}</h3>
	        <p class="muted">${esc(shortText(lesson.short_description || lesson.description, 96))}</p>
	        <span class="gold-btn free-lesson-card-cta">Сабақты ашу</span>
	      </div>
	    </article>`;
	  }

	  function bindFreeLessonCards() {
	    document.querySelectorAll("[data-free-lessons]").forEach((button) => {
	      button.addEventListener("click", openFreeLessons);
	    });
	    document.querySelectorAll("[data-free-lesson-detail]").forEach((card) => {
	      card.addEventListener("click", () => openFreeLessonDetail(card.dataset.freeLessonDetail));
	      card.addEventListener("keydown", (event) => {
	        if (event.key === "Enter" || event.key === " ") {
	          event.preventDefault();
	          openFreeLessonDetail(card.dataset.freeLessonDetail);
	        }
	      });
	    });
	  }

	  function openFreeLessons() {
	    state.freeLessonReturnScreen = state.currentScreen && state.currentScreen !== "freeLessons" ? state.currentScreen : "dashboard";
	    setScreen("freeLessons");
	  }

	  function closeFreeLessons() {
	    const target = state.freeLessonReturnScreen || "dashboard";
	    setScreen(target === "freeLessons" || target === "freeLessonDetail" ? "dashboard" : target);
	  }

	  function openFreeLessonDetail(lessonID) {
	    if (!lessonID) return;
	    state.selectedFreeLessonId = lessonID;
	    if (state.currentScreen !== "freeLessons" && state.currentScreen !== "freeLessonDetail") {
	      state.freeLessonReturnScreen = state.currentScreen || "dashboard";
	    }
	    setScreen("freeLessonDetail");
	  }

	  function closeFreeLessonDetail() {
	    state.selectedFreeLessonId = null;
	    setScreen("freeLessons");
	  }

	  async function renderFreeLessons() {
	    let lessons = state.freeLessons || [];
	    if (!lessons.length) {
	      html(`
	        <section class="screen free-lessons-screen">
	          <div class="section-head">
	            <div>
	              <p class="eyebrow">Ашық контент</p>
	              <h1>Тегін сабақтар</h1>
	            </div>
	            <button class="ghost-btn mini-back-btn" id="backFreeLessons" type="button">Артқа</button>
	          </div>
	          <div class="free-lesson-grid large">${skeletonRows(3)}</div>
	        </section>
	      `);
	      on("backFreeLessons", closeFreeLessons);
	      try {
	        const data = await api("/api/free-lessons");
	        if (state.currentScreen !== "freeLessons") return;
	        lessons = data.free_lessons || [];
	        state.freeLessons = lessons;
	      } catch (error) {
	        if (state.currentScreen !== "freeLessons") return;
	        html(`
	          <section class="screen free-lessons-screen">
	            <button class="ghost-btn mini-back-btn" id="backFreeLessons" type="button">Артқа</button>
	            ${emptyState("Сабақтарды жүктеу мүмкін болмады.")}
	            <button class="gold-btn" id="retryFreeLessons" type="button">Қайталау</button>
	          </section>
	        `);
	        on("backFreeLessons", closeFreeLessons);
	        on("retryFreeLessons", renderFreeLessons);
	        return;
	      }
	    }
	    html(`
	      <section class="screen free-lessons-screen">
	        <div class="section-head">
	          <div>
	            <p class="eyebrow">Ашық контент</p>
	            <h1>Тегін сабақтар</h1>
	          </div>
	          <button class="ghost-btn mini-back-btn" id="backFreeLessons" type="button">Артқа</button>
	        </div>
	        ${lessons.length ? `<div class="free-lesson-grid large">${lessons.map(freeLessonCard).join("")}</div>` : emptyState("Қазір тегін сабақтар жоқ.")}
	      </section>
	    `);
	    on("backFreeLessons", closeFreeLessons);
	    bindFreeLessonCards();
	  }

	  async function renderFreeLessonDetail() {
	    const lessonID = state.selectedFreeLessonId;
	    if (!lessonID) {
	      html(`<section class="screen"><button class="ghost-btn mini-back-btn" id="backFreeLessonDetail" type="button">Артқа</button>${emptyState("Сабақ табылмады.")}</section>`);
	      on("backFreeLessonDetail", closeFreeLessonDetail);
	      return;
	    }
	    html(`
	      <section class="screen free-lesson-detail-screen">
	        <button class="ghost-btn mini-back-btn" id="backFreeLessonDetail" type="button">Артқа</button>
	        <div class="card">
	          <div class="skeleton-row w-50"></div>
	          <div class="skeleton-row w-90"></div>
	          <div class="skeleton-row w-70"></div>
	        </div>
	        <div class="youtube-frame skeleton"></div>
	      </section>
	    `);
	    on("backFreeLessonDetail", closeFreeLessonDetail);
	    let lesson;
	    try {
	      const data = await api(`/api/free-lessons/${lessonID}`);
	      if (state.currentScreen !== "freeLessonDetail" || state.selectedFreeLessonId !== lessonID) return;
	      lesson = data.free_lesson;
	      if (lesson && lesson.id) {
	        const lessons = state.freeLessons || [];
	        const index = lessons.findIndex((item) => item.id === lesson.id);
	        if (index >= 0) lessons[index] = lesson;
	      }
	    } catch (error) {
	      if (state.currentScreen !== "freeLessonDetail" || state.selectedFreeLessonId !== lessonID) return;
	      html(`<section class="screen"><button class="ghost-btn mini-back-btn" id="backFreeLessonDetail" type="button">Артқа</button>${emptyState("Сабақ табылмады.")}</section>`);
	      on("backFreeLessonDetail", closeFreeLessonDetail);
	      return;
	    }
	    if (!lesson) {
	      html(`<section class="screen"><button class="ghost-btn mini-back-btn" id="backFreeLessonDetail" type="button">Артқа</button>${emptyState("Сабақ табылмады.")}</section>`);
	      on("backFreeLessonDetail", closeFreeLessonDetail);
	      return;
	    }
	    const image = visibleFreeLessonImage(lesson);
	    const embed = youtubeEmbedURL(lesson);
	    html(`
	      <section class="screen free-lesson-detail-screen">
	        <div class="section-head">
	          <button class="ghost-btn mini-back-btn" id="backFreeLessonDetail" type="button">Артқа</button>
	        </div>
	        <article class="free-lesson-detail-hero">
	          ${image ? `<img src="${esc(image)}" alt="${esc(lesson.title)}" loading="lazy" />` : ""}
	          <div>
	            <p class="eyebrow">Тегін сабақ</p>
	            <h1>${esc(lesson.title)}</h1>
	            ${lesson.short_description ? `<p class="muted">${esc(lesson.short_description)}</p>` : ""}
	          </div>
	        </article>
	        ${embed ? `<div class="youtube-frame">${youtubeIframe(embed, lesson.title)}</div>` : emptyState("Видео сілтемесі табылмады")}
	        <article class="card free-lesson-description-card">
	          <p class="eyebrow">Сипаттама</p>
	          <div class="book-description">${bookParagraphs(lesson.description)}</div>
	        </article>
	      </section>
	    `);
	    on("backFreeLessonDetail", closeFreeLessonDetail);
	  }

	  function booksHomeSection() {
	    const books = state.books || [];
	    if (!books.length) return "";
	    return `<section class="books-section" aria-label="Авторлық кітаптар">
	      <div class="section-head">
	        <div>
	          <p class="eyebrow">Кітаптар</p>
	          <h2>Авторлық кітаптар</h2>
	        </div>
	      </div>
	      <div class="book-grid">${books.map(bookCard).join("")}</div>
	    </section>`;
	  }

	  function bookCard(book) {
	    const image = visibleBookImage(book);
	    const buyUrl = bookBuyUrl(book);
	    return `<article class="book-card" data-book-detail="${esc(book.id)}" tabindex="0" role="button" aria-label="${esc(book.title)}">
	      ${image ? `<img class="book-image" src="${esc(image)}" alt="${esc(book.title)}" loading="lazy" />` : `<div class="book-image placeholder" aria-hidden="true">ZO</div>`}
	      <div class="book-card-body">
	        <h3>${esc(book.title)}</h3>
	        <p class="muted">${esc(shortText(book.description, 96))}</p>
	        <div class="book-card-foot">
	          <strong>${money(book.price_kzt)} ₸</strong>
	          <button class="gold-btn" data-buy-book="${esc(book.id)}" data-buy-url="${esc(buyUrl)}" type="button">Сатып алу</button>
	        </div>
	      </div>
	    </article>`;
	  }

	  function bindBookCards() {
	    document.querySelectorAll("[data-book-detail]").forEach((card) => {
	      card.addEventListener("click", (event) => {
	        if (event.target.closest("[data-buy-book]")) return;
	        openBookDetail(card.dataset.bookDetail);
	      });
	      card.addEventListener("keydown", (event) => {
	        if (event.key === "Enter" || event.key === " ") {
	          event.preventDefault();
	          openBookDetail(card.dataset.bookDetail);
	        }
	      });
	    });
	    document.querySelectorAll("[data-buy-book]").forEach((button) => {
	      button.addEventListener("click", (event) => {
	        event.stopPropagation();
	        const book = (state.books || []).find((item) => item.id === button.dataset.buyBook);
	        openExternalLink(button.dataset.buyUrl || bookBuyUrl(book));
	      });
	    });
	  }

	  function openBookDetail(bookID) {
	    if (!bookID) return;
	    state.selectedBookId = bookID;
	    state.bookReturnScreen = state.currentScreen && state.currentScreen !== "bookDetail" ? state.currentScreen : "dashboard";
	    setScreen("bookDetail");
	  }

	  function closeBookDetail() {
	    const target = state.bookReturnScreen || "dashboard";
	    state.selectedBookId = null;
	    setScreen(target === "bookDetail" ? "dashboard" : target);
	  }

	  async function renderBookDetail() {
	    const bookID = state.selectedBookId;
	    let book = (state.books || []).find((item) => item.id === bookID);
	    if (!book && bookID) {
	      try {
	        const data = await api(`/api/books/${bookID}`);
	        book = data.book;
	        state.whatsappSalesPhone = compact(data.whatsapp_sales_phone || state.whatsappSalesPhone);
	      } catch (error) {
	        html(`<section class="screen"><button class="ghost-btn mini-back-btn" id="backBook" type="button">Артқа</button>${emptyState(error.message || "Кітап табылмады")}</section>`);
	        on("backBook", closeBookDetail);
	        return;
	      }
	    }
	    if (!book) {
	      html(`<section class="screen"><button class="ghost-btn mini-back-btn" id="backBook" type="button">Артқа</button>${emptyState("Кітап табылмады")}</section>`);
	      on("backBook", closeBookDetail);
	      return;
	    }
	    const image = visibleBookImage(book);
	    const buyUrl = bookBuyUrl(book);
	    html(`
	      <section class="screen book-detail-screen">
	        <div class="section-head">
	          <button class="ghost-btn mini-back-btn" id="backBook" type="button">Артқа</button>
	        </div>
	        <article class="card book-detail-card">
	          ${image ? `<img class="book-detail-image" src="${esc(image)}" alt="${esc(book.title)}" loading="lazy" />` : ""}
	          <p class="eyebrow">Авторлық кітап</p>
	          <h1>${esc(book.title)}</h1>
	          <div class="price">${money(book.price_kzt)} <small>₸</small></div>
	          <div class="book-description">${bookParagraphs(book.description)}</div>
	          <button class="gold-btn lg block" id="buyBookDetail" type="button">Сатып алу</button>
	        </article>
	      </section>
	    `);
	    on("backBook", closeBookDetail);
	    on("buyBookDetail", () => openExternalLink(buyUrl));
	  }

  function metric(label, value, hint, statusKind) {
    const status = statusKind ? `<span class="status ${statusKind}">${esc(hint)}</span>` : `<span class="muted">${esc(hint || "")}</span>`;
    return `<div class="metric">
      <p class="eyebrow">${esc(label)}</p>
      <strong>${esc(String(value))}</strong>
      ${status}
    </div>`;
  }

  function renderLevelStrip() {
    if (!state.levels.length) {
      return `<div class="empty-state"><div class="icon">L</div><span>Деңгейлер жүктелуде</span></div>`;
    }
    const currentLevel = state.me && state.me.user ? state.me.user.current_level : 0;
    return `<div class="level-strip">${state.levels
      .map((level) => {
        const cls = [
          "level-token",
          level.access ? "open" : "",
          level.completed ? "completed" : "",
          level.number === currentLevel ? "current" : "",
        ]
          .filter(Boolean)
          .join(" ");
	        return `<button class="${cls}" data-level="${level.number}" type="button">
	          Деңгей
	          <b>${esc(level.number)}</b>
	          <span style="font-size:10px;letter-spacing:0;text-transform:none;color:inherit;opacity:0.85">${esc(level.title_kk || "")}</span>
	        </button>`;
      })
      .join("")}</div>`;
  }

  function renderLevels() {
    const currentLevel = (state.me && state.me.user && state.me.user.current_level) || 0;
    html(`
      <section class="screen">
        <div class="section-head">
          <div>
	          <p class="eyebrow">12 деңгейлік жол</p>
	            <h1>Менің деңгейім</h1>
	          </div>
	          <span class="pill">Деңгей ${currentLevel}</span>
        </div>
        <div class="card">${renderLevelStrip()}</div>
        <div class="grid">${state.levels.map(levelCard).join("")}</div>
      </section>
    `);
    bindLevelInviteButtons();
  }

	  function levelCard(level) {
	    const progress = level.progress || {};
	    const percent = Math.max(0, Math.min(100, num(progress.percent)));
    return `<article class="card">
      <div class="card-header">
        <div>
	          <p class="eyebrow">Деңгей ${esc(level.number)}</p>
	          <h2>${esc(level.title_kk || `Деңгей ${level.number}`)}</h2>
        </div>
        <span class="status ${level.access ? "ok" : "bad"}">${level.access ? "Ашық" : "Жабық"}</span>
      </div>
      <div class="progress-track thin"><div class="progress-fill" style="--progress:${percent}%"></div></div>
      <div class="progress-row"><span>${progress.watched_lessons || 0}/${progress.total_lessons || 0} сабақ</span><strong>${percent}%</strong></div>
      <p class="muted">${esc(progress.next_requirement || "")}</p>
	      ${level.access && level.telegram_configured ? `<button class="gold-btn block" data-level-invite="${esc(level.number)}" type="button">Жабық каналға кіру</button>` : ""}
	    </article>`;
	  }

	  function openLessonDetail(lessonID) {
	    if (!lessonID) return;
	    state.selectedLessonId = lessonID;
	    if (state.currentScreen !== "lessonDetail") {
	      state.lessonReturnScreen = state.currentScreen || "lessons";
	    }
	    setScreen("lessonDetail");
	  }

	  function closeLessonDetail() {
	    const target = state.lessonReturnScreen || "lessons";
	    setScreen(target === "lessonDetail" || target === "test" ? "lessons" : target);
	  }

	  function openTest() {
	    if (state.currentScreen !== "test") {
	      state.testReturnScreen = state.currentScreen || "lessons";
	    }
	    if (state.currentScreen !== "lessonDetail") {
	      state.selectedLessonId = null;
	    }
	    state.testResult = null;
	    state.testSelectedAnswers = {};
	    state.testResultLevel = 0;
	    setScreen("test");
	  }

	  function returnFromTest() {
	    const target = state.testReturnScreen || "lessons";
	    setScreen(target === "test" ? "lessons" : target);
	  }

	  async function renderLessons() {
	    const level = (state.me && state.me.user && state.me.user.current_level) || 1;
	    html(`<section class="screen">
	      <div class="section-head">
		        <div><p class="eyebrow">Деңгей ${esc(level)}</p><h1>Сабақтар</h1></div>
	        <div class="action-row compact">
	          <button class="ghost-btn mini-back-btn" id="backLessons" type="button">Артқа</button>
	          <button class="ghost-btn" id="refreshLessons" type="button">Жаңарту</button>
	        </div>
	      </div>
	      <div class="grid">${skeletonRows(3)}</div>
	    </section>`);
	    on("backLessons", () => setScreen("dashboard"));

	    let data;
	    try {
	      data = await api(`/api/lessons?level=${level}`);
	    } catch (error) {
	      html(`<section class="screen"><button class="ghost-btn mini-back-btn" id="backLessons" type="button">Артқа</button><h1>Сабақтарым</h1>${emptyState(error.message)}</section>`);
	      on("backLessons", () => setScreen("dashboard"));
	      return;
	    }
    state.lessons = data.lessons || [];

    const currentLevelMeta = state.levels.find((item) => Number(item.number) === Number(level));
    const telegramCard =
      currentLevelMeta && currentLevelMeta.access && currentLevelMeta.telegram_configured
        ? `<div class="card">
            <div class="card-header">
              <div><p class="eyebrow">Telegram материалдар</p><h2>Жабық канал</h2></div>
              <span class="status ok">Ашық</span>
            </div>
            <p class="muted">Осы деңгейдің қорғалған видео материалдары Telegram жабық каналында орналасады.</p>
            <button class="gold-btn block" data-level-invite="${esc(level)}" type="button">Telegram материалдарға өту</button>
          </div>`
        : "";
    const cards = state.lessons.length
      ? state.lessons.map(lessonCard).join("")
      : emptyState("Бұл деңгейге сабақтар әлі қосылған жоқ.");

	    html(`
	      <section class="screen">
	        <div class="section-head">
		          <div><p class="eyebrow">Деңгей ${esc(level)}</p><h1>Сабақтар</h1></div>
	          <div class="action-row compact">
	            <button class="ghost-btn mini-back-btn" id="backLessons" type="button">Артқа</button>
	            <button class="ghost-btn" id="refreshLessons" type="button">Жаңарту</button>
	          </div>
	        </div>
	        ${telegramCard}
	        <div class="grid">${cards}</div>
	      </section>
	    `);
	    on("backLessons", () => setScreen("dashboard"));
	    on("refreshLessons", renderLessons);
	    bindLevelInviteButtons();
	    document
	      .querySelectorAll("[data-watch]")
	      .forEach((btn) => btn.addEventListener("click", () => markWatched(btn.dataset.watch)));
	    document
	      .querySelectorAll("[data-open-lesson]")
	      .forEach((btn) => btn.addEventListener("click", () => openLessonDetail(btn.dataset.openLesson)));
	  }

	  function lessonCard(lesson) {
	    const watched = Boolean(lesson.watched);
	    const access = Boolean(lesson.access);
    return `<article class="lesson-card ${access ? "" : "locked"}">
      <div class="card-header">
        <div>
          <p class="eyebrow">Сабақ ${esc(lesson.sort_order)}</p>
          <h2>${esc(lesson.title_kk || "Сабақ")}</h2>
        </div>
        <span class="status ${watched ? "ok" : access ? "" : "bad"}">${watched ? "Көрілді" : access ? "Ашық" : "Жабық"}</span>
      </div>
		      <p class="muted">${esc(lesson.description_kk || "ZHENIS ORDA UNIVERSE")}</p>
	      <div class="lesson-actions">
	        <button class="gold-btn" data-open-lesson="${esc(lesson.id)}" ${access ? "" : "disabled"} type="button">
	          ${watched ? "Сабақты ашу" : "Сабақты бастау"}
	        </button>
	        <button class="${watched ? "ghost-btn" : "gold-btn"}" data-watch="${esc(lesson.id)}" ${access ? "" : "disabled"} type="button">
	          ${watched ? "Қайта белгілеу" : "Сабақты өттім"}
	        </button>
	      </div>
	    </article>`;
	  }

	  async function renderLessonDetail() {
	    const lessonID = state.selectedLessonId;
	    if (!lessonID) {
	      html(`<section class="screen"><button class="ghost-btn mini-back-btn" id="backLessonDetail" type="button">Артқа</button>${emptyState("Сабақ табылмады.")}</section>`);
	      on("backLessonDetail", closeLessonDetail);
	      return;
	    }
	    html(`
	      <section class="screen lesson-detail-screen">
	        <div class="section-head">
	          <button class="ghost-btn mini-back-btn" id="backLessonDetail" type="button">Артқа</button>
	        </div>
	        <div class="card">
	          <div class="skeleton-row w-50"></div>
	          <div class="skeleton-row w-90"></div>
	          <div class="skeleton-row w-70"></div>
	        </div>
	      </section>
	    `);
	    on("backLessonDetail", closeLessonDetail);
	    let lesson;
	    try {
	      const data = await api(`/api/lessons/${lessonID}`);
	      if (state.currentScreen !== "lessonDetail" || state.selectedLessonId !== lessonID) return;
	      lesson = data.lesson;
	      if (lesson && lesson.id) {
	        const lessons = state.lessons || [];
	        const index = lessons.findIndex((item) => item.id === lesson.id);
	        if (index >= 0) lessons[index] = Object.assign({}, lessons[index], lesson);
	      }
	    } catch (error) {
	      if (state.currentScreen !== "lessonDetail" || state.selectedLessonId !== lessonID) return;
	      html(`<section class="screen"><button class="ghost-btn mini-back-btn" id="backLessonDetail" type="button">Артқа</button>${emptyState(error.message || "Сабақ табылмады.")}</section>`);
	      on("backLessonDetail", closeLessonDetail);
	      return;
	    }
	    if (!lesson) {
	      html(`<section class="screen"><button class="ghost-btn mini-back-btn" id="backLessonDetail" type="button">Артқа</button>${emptyState("Сабақ табылмады.")}</section>`);
	      on("backLessonDetail", closeLessonDetail);
	      return;
	    }
	    const watched = Boolean(lesson.watched);
	    html(`
	      <section class="screen lesson-detail-screen">
	        <div class="section-head">
	          <button class="ghost-btn mini-back-btn" id="backLessonDetail" type="button">Артқа</button>
	        </div>
	        <article class="card lesson-detail-card">
	          <div class="card-header">
	            <div>
	              <p class="eyebrow">Деңгей ${esc(lesson.level_number)} · Сабақ ${esc(lesson.sort_order)}</p>
	              <h1>${esc(lesson.title_kk || "Сабақ")}</h1>
	            </div>
	            <span class="status ${watched ? "ok" : ""}">${watched ? "Көрілді" : "Ашық"}</span>
	          </div>
	          <p class="muted">${esc(lesson.description_kk || "ZHENIS ORDA UNIVERSE")}</p>
	          <div class="lesson-detail-actions">
	            ${lesson.video_url ? `<button class="ghost-btn block" id="openLessonMaterial" type="button">Материалды ашу</button>` : ""}
	            <button class="${watched ? "ghost-btn" : "gold-btn"} block" id="markLessonWatched" type="button">${watched ? "Қайта белгілеу" : "Сабақты өттім"}</button>
	            <button class="gold-btn block" id="openLessonTest" type="button">Тестке өту</button>
	          </div>
	        </article>
	      </section>
	    `);
	    on("backLessonDetail", closeLessonDetail);
	    on("openLessonMaterial", () => openExternalLink(lesson.video_url));
	    on("markLessonWatched", () => markWatched(lesson.id));
	    on("openLessonTest", openTest);
	  }

	  async function markWatched(id) {
	    try {
	      await api(`/api/lessons/${id}/watched`, { method: "POST", body: "{}" });
      const me = await api("/api/me");
	      state.me = me;
	      await refreshLevels();
	      toast("Сабақ белгіленді", "success");
	      if (state.currentScreen === "lessonDetail") renderLessonDetail();
	      else renderLessons();
	    } catch (error) {
	      toast(error.message || "Жаңарту мүмкін болмады", "error");
	    }
  }

  function bindLevelInviteButtons() {
    document.querySelectorAll("[data-level-invite]").forEach((button) => {
      button.addEventListener("click", () => openLevelTelegramInvite(button));
    });
  }

  async function openLevelTelegramInvite(button) {
    if (!button || button.dataset.busy === "1") return;
    const level = button.dataset.levelInvite;
    button.dataset.busy = "1";
    const oldText = button.textContent;
    button.textContent = "Сілтеме дайындалуда...";
    button.disabled = true;
    try {
      const res = await api(`/api/levels/${level}/telegram-invite`, {
        method: "POST",
        body: "{}",
      });
      openTelegramLink(res.invite_link);
      toast("Telegram шақыру сілтемесі дайын", "success");
    } catch (error) {
      toast(error.message || "Telegram сілтемесін алу мүмкін болмады", "error");
    } finally {
      button.dataset.busy = "";
      button.textContent = oldText;
      button.disabled = false;
    }
  }

  function openTelegramLink(link) {
    const tg = getTelegram();
    if (tg && typeof tg.openTelegramLink === "function") {
      tg.openTelegramLink(link);
      return;
    }
    if (tg && typeof tg.openLink === "function") {
      tg.openLink(link);
      return;
    }
    window.open(link, "_blank", "noopener");
  }

	  async function renderTest() {
	    const level = state.testResultLevel || (state.me && state.me.user && state.me.user.current_level) || 1;
	    let data;
	    try {
	      data = await api(`/api/tests/${level}`);
	    } catch (error) {
	      html(`<section class="screen"><div class="section-head"><button class="ghost-btn mini-back-btn" id="backTest" type="button">Артқа</button></div>${emptyState(error.message || "Тест әлі ашылмаған")}</section>`);
	      on("backTest", returnFromTest);
	      return;
	    }
	    const test = data.test;
	    if (!test) {
	      html(`<section class="screen"><button class="ghost-btn mini-back-btn" id="backTest" type="button">Артқа</button><h1>Тест</h1>${emptyState("Бұл деңгейге тест әлі қосылған жоқ.")}</section>`);
	      on("backTest", returnFromTest);
	      return;
	    }
	    if (state.testResult && Number(state.testResultLevel) === Number(level) && resultAttempt(state.testResult).test_id === test.id) {
	      renderTestResult(test, state.testResult, state.testSelectedAnswers);
	      return;
	    }
	    html(`
	      <section class="screen">
	        <div class="section-head">
	          <button class="ghost-btn mini-back-btn" id="backTest" type="button">Артқа</button>
	        </div>
	        <div class="card">
		          <p class="eyebrow">${test.lesson_title_kk ? `Сабақ: ${esc(test.lesson_title_kk)}` : `Деңгей ${esc(level)}`}</p>
	          <h1>${esc(test.title)}</h1>
	          <p class="muted">Өту шегі: ${esc(test.pass_percent)}%</p>
	        </div>
        <form id="testForm" class="form">
          ${(test.questions || []).map(questionBlock).join("")}
          <button class="gold-btn lg" type="submit"><span class="btn-label">Тест тапсыру</span><span class="btn-spinner"></span></button>
	        </form>
	      </section>
	    `);
	    on("backTest", returnFromTest);
	    document.getElementById("testForm").addEventListener("submit", async (event) => {
	      event.preventDefault();
	      const submitBtn = event.currentTarget.querySelector("button[type=submit]");
	      if (buttonIsLoading(submitBtn)) return;
	      setButtonLoading(submitBtn, true);
      const answers = {};
      new FormData(event.currentTarget).forEach((value, key) => (answers[key] = String(value)));
      try {
        const result = await api(`/api/tests/${level}/submit`, {
	          method: "POST",
	          body: JSON.stringify({ answers }),
	        });
	        state.testResult = result;
	        state.testSelectedAnswers = answers;
	        state.testResultLevel = test.level_number || level;
	        renderTestResult(test, result, answers);
	        refreshStudentProgressAfterTest();
	        const attempt = resultAttempt(result);
	        toast(`Нәтиже: ${attempt.score_percent}% — ${attempt.passed ? "Сәтті" : "Қайталау"}`, attempt.passed ? "success" : "error");
	      } catch (error) {
	        toast(error.message || "Тест жіберу мүмкін болмады", "error");
	      } finally {
	        setButtonLoading(submitBtn, false);
	      }
	    });
	  }

	  function questionBlock(question) {
	    return `<fieldset class="card test-question-card">
	      <h3>${esc(question.question_text_kk)}</h3>
	      ${(question.options || [])
	        .map(
	          (option) => `<label class="test-option">
	            <input name="${esc(question.id)}" value="${esc(option.id)}" type="radio" required />
            <span>${esc(option.option_text_kk)}</span>
          </label>`,
        )
	        .join("")}
	    </fieldset>`;
	  }

	  function renderTestResult(test, submitResult, selectedAnswers) {
	    const attempt = resultAttempt(submitResult);
	    const results = resultMap(submitResult);
	    const passed = Boolean(attempt.passed);
	    const score = num(attempt.score_percent);
	    const correct = num(attempt.correct_count);
	    const total = num(attempt.total_count || (test.questions || []).length);
	    const passPercent = num(attempt.pass_percent || submitResult.pass_percent || test.pass_percent);
	    const progress = submitResult.progress || {};
	    const unlockedNext = passed && Number(progress.level_number || 0) > Number(test.level_number || 0);
	    html(`
	      <section class="screen test-result-screen">
	        <div class="section-head">
	          <button class="ghost-btn mini-back-btn" id="backTest" type="button">Артқа</button>
	        </div>
	        <article id="testResultSummary" class="card test-result-card ${passed ? "passed" : "failed"}">
	          <div class="card-header">
	            <div>
	              <p class="eyebrow">${test.lesson_title_kk ? `Сабақ: ${esc(test.lesson_title_kk)}` : `Деңгей ${esc(test.level_number || state.testResultLevel || "")}`}</p>
	              <h1>${passed ? "Тест сәтті өтті ✅" : "Тесттен өтпедіңіз"}</h1>
	            </div>
	            <span class="status ${passed ? "ok" : "bad"}">${passed ? "Өтті" : "Қайталау қажет"}</span>
	          </div>
	          <p class="muted">${passed ? "Жарайсыз! Нәтижеңіз сақталды." : "Ештеңе етпейді. Дұрыс жауаптарды қарап, қайта тапсырып көріңіз."}</p>
	          ${unlockedNext ? `<p class="test-unlock-note">Келесі деңгей ашылды.</p>` : ""}
	          <div class="test-result-stats">
	            <div><span>Нәтиже</span><strong>${esc(score)}%</strong></div>
	            <div><span>Дұрыс жауаптар</span><strong>${esc(correct)}/${esc(total)}</strong></div>
	            <div><span>Өту балы</span><strong>${esc(passPercent)}%</strong></div>
	          </div>
	        </article>
	        <div class="test-answer-review">
	          ${(test.questions || []).map((question) => resultQuestionBlock(question, results[question.id], selectedAnswers)).join("")}
	        </div>
	        <div class="test-result-actions">
	          <button class="ghost-btn" id="backToLesson" type="button">Сабаққа қайту</button>
	          <button class="ghost-btn" id="backToLevels" type="button">Деңгейлерге қайту</button>
	          <button class="gold-btn" id="retryTest" type="button">Қайта тапсыру</button>
	        </div>
	      </section>
	    `);
	    on("backTest", returnFromTest);
	    on("backToLesson", () => {
	      if (state.selectedLessonId) setScreen("lessonDetail");
	      else setScreen("lessons");
	    });
	    on("backToLevels", () => setScreen("levels"));
	    on("retryTest", () => {
	      state.testResult = null;
	      state.testSelectedAnswers = {};
	      renderTest();
	    });
	    const summary = document.getElementById("testResultSummary");
	    if (summary && typeof summary.scrollIntoView === "function") {
	      summary.scrollIntoView({ behavior: "smooth", block: "start" });
	    }
	  }

	  function resultQuestionBlock(question, result, selectedAnswers) {
	    const selectedID = (result && result.selected_option_id) || (selectedAnswers && selectedAnswers[question.id]) || "";
	    const correctID = (result && result.correct_option_id) || "";
	    return `<fieldset class="card test-question-card reviewed">
	      <h3>${esc(question.question_text_kk)}</h3>
	      ${(question.options || [])
	        .map((option) => resultOptionHtml(option, selectedID, correctID))
	        .join("")}
	    </fieldset>`;
	  }

	  function resultOptionHtml(option, selectedID, correctID) {
	    const selected = option.id === selectedID;
	    const correct = option.id === correctID;
	    const wrong = selected && !correct;
	    const cls = ["test-option", "reviewed", selected ? "selected" : "", correct ? "correct" : "", wrong ? "wrong" : ""]
	      .filter(Boolean)
	      .join(" ");
	    const badges = [
	      selected ? `<span class="answer-badge yours">Сіздің жауабыңыз</span>` : "",
	      correct ? `<span class="answer-badge correct">Дұрыс жауап</span>` : "",
	    ].join("");
	    return `<label class="${cls}">
	      <input value="${esc(option.id)}" type="radio" ${selected ? "checked" : ""} disabled />
	      <span>${esc(option.option_text_kk)}${badges ? `<em>${badges}</em>` : ""}</span>
	    </label>`;
	  }

	  function resultAttempt(result) {
	    const attempt = (result && result.attempt) || {};
	    return Object.assign({}, attempt, {
	      score_percent: attempt.score_percent != null ? attempt.score_percent : result && result.score_percent ? result.score_percent : 0,
	      correct_count: attempt.correct_count != null ? attempt.correct_count : result && result.correct_count ? result.correct_count : 0,
	      total_count: attempt.total_count != null ? attempt.total_count : result && result.total_count ? result.total_count : 0,
	      pass_percent: attempt.pass_percent != null ? attempt.pass_percent : result && result.pass_percent ? result.pass_percent : 0,
	      passed: attempt.passed != null ? attempt.passed : Boolean(result && result.passed),
	      test_id: attempt.test_id || (result && result.test_id) || "",
	    });
	  }

	  function resultMap(result) {
	    const list = (result && result.results) || (result && result.attempt && result.attempt.results) || [];
	    return list.reduce((acc, item) => {
	      if (item && item.question_id) acc[item.question_id] = item;
	      return acc;
	    }, {});
	  }

	  async function refreshStudentProgressAfterTest() {
	    try {
	      const me = await api("/api/me");
	      state.me = me;
	    } catch (_) {}
	    await refreshLevels();
	  }

  async function renderAssignment() {
    const level = (state.me && state.me.user && state.me.user.current_level) || 1;
    let assignment;
    try {
      assignment = (await api(`/api/assignments/${level}`)).assignment;
    } catch (error) {
      html(`<section class="screen"><h1>Тапсырмаларым</h1>${emptyState("Бұл деңгейде тапсырма жоқ немесе әлі ашылмаған.")}</section>`);
      return;
    }
    if (!assignment) {
      html(`<section class="screen"><h1>Тапсырмаларым</h1>${emptyState("Бұл деңгейде тапсырма жоқ")}</section>`);
      return;
    }
    html(`
      <section class="screen">
        <div class="card">
	          <p class="eyebrow">Деңгей ${esc(level)}</p>
          <h1>${esc(assignment.title_kk)}</h1>
          <p class="muted">${esc(assignment.description_kk || "")}</p>
        </div>
        <form id="assignmentForm" class="form">
          <label class="field"><span>Жауап</span><textarea name="answer_text" required placeholder="Тапсырма жауабын жазыңыз..."></textarea></label>
          <label class="field"><span>Сілтеме</span><input name="link_url" placeholder="https://" /></label>
          <button class="gold-btn lg" type="submit"><span class="btn-label">Жіберу</span><span class="btn-spinner"></span></button>
        </form>
      </section>
    `);
    document.getElementById("assignmentForm").addEventListener("submit", async (event) => {
      event.preventDefault();
      const submitBtn = event.currentTarget.querySelector("button[type=submit]");
      if (buttonIsLoading(submitBtn)) return;
      setButtonLoading(submitBtn, true);
      try {
        const body = Object.fromEntries(new FormData(event.currentTarget).entries());
        await api(`/api/assignments/${level}/submit`, {
          method: "POST",
          body: JSON.stringify(body),
        });
        const me = await api("/api/me");
        state.me = me;
        toast("Тапсырма жіберілді", "success");
        setScreen("dashboard");
      } catch (error) {
        toast(error.message || "Жіберу мүмкін болмады", "error");
      } finally {
        setButtonLoading(submitBtn, false);
      }
    });
  }

  function renderDiagnostics() {
    const fields = [
      ["name", "Атыңыз"],
      ["city", "Қалаңыз"],
      ["age", "Жасыңыз"],
      ["income", "Қазіргі табысыңыз"],
      ["has_debt", "Қарызыңыз бар ма?"],
      ["has_business", "Бизнесіңіз бар ма?"],
      ["main_problem", "Негізгі проблемаңыз қандай?"],
      ["growth_area", "Қай салада өскіңіз келеді?"],
    ];
    html(`
      <section class="screen">
        <div class="card">
          <p class="eyebrow">Тегін диагностика</p>
          <h1>Диагностика</h1>
          <p class="muted">Нәтижеден кейін жүйе сізге бірінші іргетасты ұсынады.</p>
        </div>
        <form id="diagnosticsForm" class="form">
          ${fields.map(([key, label]) => `<label class="field"><span>${esc(label)}</span><input name="${key}" required /></label>`).join("")}
          <button class="gold-btn lg" type="submit"><span class="btn-label">Жіберу</span><span class="btn-spinner"></span></button>
        </form>
      </section>
    `);
    document.getElementById("diagnosticsForm").addEventListener("submit", async (event) => {
      event.preventDefault();
      const submitBtn = event.currentTarget.querySelector("button[type=submit]");
      if (buttonIsLoading(submitBtn)) return;
      setButtonLoading(submitBtn, true);
      try {
        const answers = Object.fromEntries(new FormData(event.currentTarget).entries());
        const res = await api("/api/diagnostics", {
          method: "POST",
          body: JSON.stringify({ answers }),
        });
        toast(res.message || "Диагностика сақталды", "success");
        setScreen("tariffs");
      } catch (error) {
        toast(error.message || "Жіберу мүмкін болмады", "error");
      } finally {
        setButtonLoading(submitBtn, false);
      }
    });
  }

	  function renderFinancialIq() {
	    html(`
	      <section class="screen financial-iq-screen">
	        <div class="section-head">
	          <button class="ghost-btn mini-back-btn" id="backFinancialIq" type="button">Артқа</button>
	        </div>
	        <div class="card">
	          <p class="eyebrow">Қаржылық IQ тесті</p>
	          <h1>Қаржылық IQ тесті</h1>
	          <p class="muted">Тесті толтырып, қаржылық деңгейіңізді анықтаңыз.</p>
	        </div>
	        <form id="financialIqForm" class="form iq-form">
	          ${financialIqQuestions.map(financialIqQuestionHtml).join("")}
	          <button class="gold-btn lg" type="submit"><span class="btn-label">Нәтижені көру</span><span class="btn-spinner"></span></button>
	        </form>
	      </section>
	    `);
	    on("backFinancialIq", returnFromFinancialIq);
	    document.getElementById("financialIqForm").addEventListener("submit", async (event) => {
	      event.preventDefault();
	      const submitBtn = event.currentTarget.querySelector("button[type=submit]");
	      if (buttonIsLoading(submitBtn)) return;
	      setButtonLoading(submitBtn, true);
	      calculateFinancialIq(event.currentTarget);
	      const result = state.financialIqResult;
	      try {
	        const saved = await api("/api/financial-iq", {
	          method: "POST",
	          body: JSON.stringify({
	            score: result.score,
	            result_title: result.title,
	            result_level: result.level,
	            result_text: result.text,
	            answers: state.financialIqAnswers,
	          }),
	        });
	        state.me = Object.assign({}, state.me || {}, { financial_iq: saved.financial_iq });
	        toast(saved.message || `Сіз қаржылық IQ тестін аяқтадыңыз. Нәтижеңіз: ${result.score}`, "success");
	        setScreen("financialIqResult");
	      } catch (error) {
	        toast(error.message || "Нәтижені сақтау мүмкін болмады", "error");
	      } finally {
	        setButtonLoading(submitBtn, false);
	      }
	    });
	  }

	  function renderFinancialIqResult() {
	    const result = financialIqResultForView(state.financialIqResult || savedFinancialIqResult());
	    if (!result) {
	      renderFinancialIq();
	      return;
	    }
	    html(`
	      <section class="screen financial-iq-screen">
	        <div class="section-head">
	          <button class="ghost-btn mini-back-btn" id="backFinancialIqResult" type="button">Артқа</button>
	        </div>
	        <div class="card iq-result-card">
	          <div class="iq-result-icon" aria-hidden="true">IQ</div>
	          <p class="eyebrow">Нәтиже</p>
	          <h1>Сіздің нәтижеңіз: ${esc(result.score)} балл</h1>
	          <span class="pill">${esc(result.title)}</span>
	          <h2>${esc(result.level)}</h2>
	          <div class="iq-result-text">
	            ${String(result.text)
	              .split("\n\n")
	              .map((paragraph) => `<p class="muted">${esc(paragraph)}</p>`)
	              .join("")}
	          </div>
	        </div>
	        <div class="grid two tight">
	          <button class="gold-btn lg" id="finishFinancialIq" type="button">Басты бетке оралу</button>
	          <button class="ghost-btn lg" id="retryFinancialIq" type="button">Қайта тапсыру</button>
	        </div>
	      </section>
	    `);
	    on("backFinancialIqResult", returnFromFinancialIq);
	    on("finishFinancialIq", returnFromFinancialIq);
	    on("retryFinancialIq", openFinancialIq);
	  }

	  async function renderTariffs() {
	    try {
	      const data = await api("/api/tariffs");
	      state.platform = Object.assign({}, state.platform || {}, { tariffs: data.tariffs || [] });
	    } catch (error) {
	      toast(error.message, "error");
	      state.platform = Object.assign({}, state.platform || {}, { tariffs: [] });
	    }
	    const tariffs = (state.platform && state.platform.tariffs) || [];
	    html(`
	      <section class="screen">
	        <div class="card">
	          <p class="eyebrow">Жазылым</p>
	          <h1>Тарифтер</h1>
	          <p class="muted">1-деңгей төлемнен кейін ашылады. Контент саты-саты беріледі.</p>
	        </div>
	        <div class="grid three">${tariffs.length ? tariffs.map(tariffCard).join("") : emptyState("Қазір қолжетімді тариф жоқ.")}</div>
	      </section>
	    `);
	    document.querySelectorAll("[data-tariff]").forEach((button) =>
	      button.addEventListener("click", () => {
	        state.selectedTariff = button.dataset.tariff;
        setScreen("payment");
      }),
    );
	  }

	  function tariffCard(tariff) {
	    const image = visibleTariffImage(tariff);
	    return `<article class="tariff-card ${tariff.code === "STANDARD" ? "featured" : ""}">
	      ${image ? `<img class="tariff-image" src="${esc(image)}" alt="${esc(tariff.title || tariff.code)}" loading="lazy" />` : ""}
	      <header>
	        <div>
	          <p class="eyebrow">${esc(tariff.code || "")}</p>
	          <h2>${esc(tariff.title || tariff.code)}</h2>
	          <div class="price">${money(tariff.price_kzt)} <small>₸ / ай</small></div>
	        </div>
	        <span class="pill">${esc(tariff.code)}</span>
	      </header>
	      ${tariff.short_description_kk ? `<p class="muted">${esc(tariff.short_description_kk)}</p>` : ""}
	      ${tariffBenefitsHtml(tariff.features, 5)}
	      <button class="gold-btn block" data-tariff="${esc(tariff.id || tariff.code)}" type="button">Тариф сатып алу</button>
	    </article>`;
	  }

	  function tariffBenefitsHtml(features, limit) {
	    const all = features || [];
	    const items = all.slice(0, limit || all.length);
	    return items.length ? `<ul>${items.map((item) => `<li>${esc(item)}</li>`).join("")}</ul>` : "";
	  }

	  function renderPayment() {
	    const tariff = selectedTariff();
	    if (!tariff) {
	      html(`<section class="screen"><button class="ghost-btn mini-back-btn" id="backTariffs" type="button">Артқа</button>${emptyState("Қазір қолжетімді тариф жоқ.")}</section>`);
	      on("backTariffs", () => setScreen("tariffs"));
	      return;
	    }
	    const image = visibleTariffImage(tariff);
	    html(`
	      <section class="screen">
	        <div class="section-head">
	          <button class="ghost-btn mini-back-btn" id="backTariffs" type="button">Артқа</button>
	        </div>
	        <div class="card">
	          <p class="eyebrow">Төлем</p>
	          <h1>${esc(tariff.title || tariff.code)}</h1>
	          ${image ? `<img class="tariff-detail-image" src="${esc(image)}" alt="${esc(tariff.title || tariff.code)}" loading="lazy" />` : ""}
	          <div class="price">${money(tariff.price_kzt)} <small>₸ / ай</small></div>
	          ${tariff.full_description_kk ? `<p class="muted">${esc(tariff.full_description_kk)}</p>` : tariff.short_description_kk ? `<p class="muted">${esc(tariff.short_description_kk)}</p>` : ""}
	          ${tariffBenefitsHtml(tariff.features)}
	          <p class="muted">Kaspi арқылы төлем жасағаннан кейін PDF-чекті Telegram ботқа жіберіңіз.</p>
	        </div>
	        <form id="paymentForm" class="form">
	          ${staticKaspiMethodHtml()}
	          <button class="gold-btn lg" type="submit"><span class="btn-label">Тариф сатып алу</span><span class="btn-spinner"></span></button>
	        </form>
	        <div id="paymentResult"></div>
	      </section>
	    `);
	    on("backTariffs", () => setScreen("tariffs"));
	    document.getElementById("paymentForm").addEventListener("submit", async (event) => {
	      event.preventDefault();
	      const submitBtn = event.currentTarget.querySelector("button[type=submit]");
	      if (buttonIsLoading(submitBtn)) return;
	      setButtonLoading(submitBtn, true);
	      try {
	        const provider = DEFAULT_PAYMENT_PROVIDER;
	        const contactPhone = await openPurchasePhoneSheet({
	          title: tariff.title || tariff.code,
	          amount: tariff.price_kzt,
	        });
	        if (!contactPhone) return;
	        const res = await runAfterLegalAgreement(() =>
	          api("/api/payments", {
	            method: "POST",
	            body: JSON.stringify({ tariff_id: tariff.id, tariff_code: tariff.code, provider, contact_phone: contactPhone }),
	          }),
	        );
	        if (!res) return;
	        if (state.me && state.me.user && res.payment && res.payment.contact_phone) {
	          state.me.user.phone = res.payment.contact_phone;
	        }
	        document.getElementById("paymentResult").innerHTML = `
	          <div class="card">
	            <p class="eyebrow">Төлем құрылды</p>
	            <h2>Төлем күтіліп тұр</h2>
            <p class="muted">Kaspi арқылы төлем жасап, PDF-чекті Telegram ботқа жіберіңіз.</p>
            <p>Сома: <strong>${money(res.payment.amount_kzt)} ₸</strong></p>
          </div>
          ${receiptUploadHtml(res.payment)}
        `;
	        bindReceiptUpload(res.payment.id);
	        await openPaymentInstructionSheet(res.payment, res.instructions, provider);
	        toast("Төлем құрылды", "success");
	      } catch (error) {
	        toast(error.message || "Төлем жасау мүмкін болмады", "error");
	      } finally {
	        setButtonLoading(submitBtn, false);
	      }
	    });
  }

  function receiptUploadHtml(payment) {
    return `
      <div class="card receipt-upload-card">
        <div class="card-header">
          <div>
            <p class="eyebrow">Түбіртек жүктеу</p>
            <h2>PDF-чекті жіберіңіз</h2>
          </div>
          <span class="status warn">Тексеріледі</span>
        </div>
        <form id="receiptUploadForm" class="form">
          <label class="upload-drop">
            <input name="receipt" type="file" accept=".pdf,application/pdf" required />
            <span class="upload-title">PDF-чекті жүктеу</span>
            <small>PDF құжат</small>
            <strong id="receiptFileName">Файл таңдалмады</strong>
          </label>
          <button class="gold-btn lg" type="submit">
            <span class="btn-label">Тексеруге жіберу</span><span class="btn-spinner"></span>
          </button>
        </form>
        <div id="receiptUploadState" class="muted small">PDF-чекті Telegram ботқа жіберіңіз. Қажет болса осы жерден де жүктей аласыз.</div>
      </div>
    `;
  }

  function bindReceiptUpload(paymentID) {
    const form = document.getElementById("receiptUploadForm");
    if (!form) return;
    const input = form.querySelector("input[type=file]");
    const fileName = document.getElementById("receiptFileName");
    input.addEventListener("change", () => {
      fileName.textContent = input.files && input.files[0] ? input.files[0].name : "Файл таңдалмады";
    });
    form.addEventListener("submit", async (event) => {
      event.preventDefault();
      const submitBtn = form.querySelector("button[type=submit]");
      const stateNode = document.getElementById("receiptUploadState");
      if (buttonIsLoading(submitBtn)) return;
      setButtonLoading(submitBtn, true);
      try {
        const fd = new FormData(form);
        const res = await api(`/api/payments/${paymentID}/receipt`, {
          method: "POST",
          body: fd,
        });
        stateNode.innerHTML = `${statusBadge(res.receipt.validation_status)} <span>Түбіртек тексеруге жіберілді</span>`;
        toast("Түбіртек тексеруге жіберілді", "success");
      } catch (error) {
        stateNode.textContent = error.message || "Түбіртек жүктеу мүмкін болмады";
        toast(error.message || "Түбіртек жүктеу мүмкін болмады", "error");
      } finally {
        setButtonLoading(submitBtn, false);
      }
    });
  }

  async function renderReferral() {
    html(`<section class="screen"><div class="card skeleton" style="min-height:140px"></div><div class="grid two"><div class="metric skeleton"></div><div class="metric skeleton"></div></div></section>`);
    let referral;
    try {
      const data = await api("/api/referral");
      referral = data.referral;
      state.referral = referral;
	    } catch (error) {
	      html(`<section class="screen"><h1>Дос шақыру</h1>${emptyState(error.message)}</section>`);
	      return;
	    }
	    html(`
	      <section class="screen">
	        <div class="hero">
	          <p class="eyebrow">Дос шақыру</p>
	          <h1>Дос шақыру</h1>
	          <p class="muted">Досыңызды жеке сілтемеңіз арқылы шақырыңыз. Ол тіркеліп, төлем жасағаннан кейін бонус беріледі.</p>
	        </div>
	        <div class="card">
	          <p class="eyebrow">Сілтеме</p>
	          <div class="referral-link">${esc(referral.referral_link || "")}</div>
	          <div class="grid two tight">
	            <button class="gold-btn" id="copyRef" type="button">Сілтемені көшіру</button>
	            <button class="ghost-btn" id="shareRef" type="button">Telegram-да бөлісу</button>
	          </div>
        </div>
        <div class="grid two">
          ${metric("Шақырылған", referral.invited_count || 0, "тіркелді")}
          ${metric("Төлем жасаған", referral.paid_count || 0, "мақұлданды")}
        </div>
      </section>
	    `);
	    on("copyRef", () => {
	      copyText(referral.referral_link).then(() => toast("Сілтеме көшірілді", "success")).catch(() => toast("Көшіру қолжетімді емес", "error"));
	    });
    on("shareRef", () => {
      const tg = getTelegram();
	      const text = encodeURIComponent("ZHENIS ORDA UNIVERSE — жүйелі даму платформасы. " + referral.referral_link);
      const url = `https://t.me/share/url?url=${encodeURIComponent(referral.referral_link)}&text=${text}`;
      if (tg && typeof tg.openTelegramLink === "function") tg.openTelegramLink(url);
      else window.open(url, "_blank");
    });
  }

  async function renderCoins() {
    html(`<section class="screen"><div class="card skeleton" style="min-height:140px"></div></section>`);
    let coins;
    let bonuses;
    try {
      [coins, bonuses] = await Promise.all([api("/api/coins"), api("/api/bonuses")]);
    } catch (error) {
      html(`<section class="screen"><h1>ZHENIS Coin</h1>${emptyState(error.message)}</section>`);
      return;
    }
    state.coins = coins;
    html(`
      <section class="screen">
        <div class="hero">
          <p class="eyebrow">ZHENIS COIN</p>
          <h1>${money(coins.balance)} <small style="color:var(--gold-soft);font-size:18px">coin</small></h1>
          <p class="muted">Сабақ көрілді +5, тест тапсырылды +20, эфирге қатысты +30, реферал +100.</p>
        </div>
        <div class="card">
	          <p class="eyebrow">Дос шақыру бонустары</p>
          <h2>Жоспар</h2>
          <div class="grid">${(bonuses.plan || []).map((item) => `<div class="card" style="margin:0;padding:14px"><strong style="color:var(--gold-bright)">${esc(item.count)} шақыру</strong><p class="muted" style="margin-top:4px">${esc(item.reward)}</p></div>`).join("")}</div>
        </div>
      </section>
    `);
  }

  async function renderStreams() {
    html(`<section class="screen"><div class="card skeleton" style="min-height:140px"></div></section>`);
    let data;
    try {
      data = await api("/api/streams");
    } catch (error) {
      html(`<section class="screen"><h1>Жабық эфир</h1>${emptyState(error.message)}</section>`);
      return;
    }
    const streams = data.streams || [];
    html(`
      <section class="screen">
        <div class="card">
          <p class="eyebrow">ZHABYQ RAZBOR NIGHT</p>
          <h1>Жабық эфир</h1>
          <p class="muted">STANDARD және VIP үшін жазбалар эфирден кейін ашылады.</p>
        </div>
        <div class="grid">${
          streams.length
            ? streams
                .map(
                  (stream) => `<div class="card">
                    <div class="card-header">
                      <div><p class="eyebrow">${formatDateTime(stream.starts_at)}</p><h2>${esc(stream.title)}</h2></div>
                      <span class="pill">${esc(stream.tariff_requirement)}</span>
                    </div>
                    <p class="muted">${esc(stream.description || "")}</p>
                  </div>`,
                )
                .join("")
            : emptyState("Эфир әлі жоспарланбаған")
        }</div>
      </section>
    `);
  }

  async function renderChannels() {
    html(`<section class="screen"><div class="card skeleton" style="min-height:140px"></div></section>`);
    let data;
    try {
      data = await api("/api/channels");
    } catch (error) {
      html(`<section class="screen"><h1>Жабық арналар</h1>${emptyState(error.message)}</section>`);
      return;
    }
    const channels = data.channels || [];
    html(`
      <section class="screen">
        <div class="card">
          <p class="eyebrow">Жабық қол жетімділік</p>
          <h1>Жабық арналар</h1>
        </div>
        <div class="grid">${
          channels.length
            ? channels
                .map(
                  (channel) => `<div class="card">
                    <div class="card-header">
	                      <div><h2>${esc(channel.title)}</h2><p class="muted">${esc(channel.tariff_requirement)} · Деңгей ${esc(channel.level_requirement)}</p></div>
                      <span class="status ${channel.access ? "ok" : "bad"}">${channel.access ? "Ашық" : "Жабық"}</span>
                    </div>
                    <button class="gold-btn block" data-invite="${esc(channel.id)}" ${channel.access ? "" : "disabled"} type="button">Сілтеме алу</button>
                  </div>`,
                )
                .join("")
            : emptyState("Арналар жоқ")
        }</div>
      </section>
    `);
    document.querySelectorAll("[data-invite]").forEach((button) =>
      button.addEventListener("click", async () => {
        try {
          const res = await api(`/api/channels/${button.dataset.invite}/invite`, {
            method: "POST",
            body: "{}",
          });
          await modal({
            title: "Шақыру сілтемесі",
            body: `<p class="muted">Сілтеме 24 сағат жарамды.</p><div class="referral-link">${esc(res.invite_link)}</div>`,
            actions: [{ label: "Жабу", value: "ok", primary: true }],
          });
        } catch (error) {
          toast(error.message || "Арна жабық", "error");
        }
      }),
    );
  }

  function renderProfile() {
    const user = (state.me && state.me.user) || {};
    const sub = user.subscription || {};
    const subOk = sub.status === "active";
    const iq = savedFinancialIqResult();
    const display = buildDisplayName(readTelegramUser(), user);
    const login = buildLogin(readTelegramUser(), user);

    html(`
      <section class="screen">
        <div class="hero">
          <p class="eyebrow">Жеке кабинет</p>
          <h1>${esc(display)}</h1>
          <p class="muted">${esc(login)}</p>
        </div>
        <div class="grid two">
          ${metric("Қазіргі тариф", sub.tariff_code || "Жоқ", statusText[sub.status] || sub.status || "Белсенді емес", subOk ? "ok" : "warn")}
          ${metric("Мерзімі", sub.expires_at ? formatDate(sub.expires_at) : "—", "жазылым")}
	          ${metric("Қазіргі деңгей", `Деңгей ${user.current_level || 0}`, "12 деңгейлік жол")}
          ${metric("Coin балансы", money(user.coin_balance), "ZHENIS COIN")}
          ${metric("Қаржылық IQ", iq ? `${iq.score} балл` : "—", iq ? iq.result_level : "тест нәтижесі", iq ? "ok" : "warn")}
        </div>
        <div class="grid two">
	          <button class="ghost-btn" data-next="referral" type="button">Дос шақыру</button>
          <button class="ghost-btn" data-next="support" type="button">Қолдау қызметі</button>
        </div>
        <button class="ghost-btn lg block whatsapp-action" id="profileWhatsappSupport" type="button">
          <span class="whatsapp-action-mark" aria-hidden="true">
            <svg viewBox="0 0 32 32" width="16" height="16" focusable="false" aria-hidden="true">
              <path fill="currentColor" d="M16.02 5.333c-5.9 0-10.687 4.787-10.687 10.687 0 1.88.493 3.713 1.427 5.333L5.333 26.667l5.46-1.4a10.66 10.66 0 0 0 5.227 1.36h.004c5.899 0 10.687-4.788 10.687-10.687 0-2.854-1.111-5.535-3.129-7.553a10.617 10.617 0 0 0-7.562-3.054Zm0 19.45h-.003a8.86 8.86 0 0 1-4.517-1.237l-.324-.192-3.24.829.866-3.16-.211-.336a8.86 8.86 0 0 1-1.358-4.717c0-4.9 3.987-8.887 8.89-8.887 2.373 0 4.604.926 6.281 2.606a8.829 8.829 0 0 1 2.605 6.288c0 4.9-3.987 8.806-8.989 8.806Zm5.124-6.624c-.281-.141-1.663-.821-1.92-.914-.257-.094-.444-.141-.631.141-.187.281-.722.914-.886 1.101-.164.187-.327.21-.609.07-.281-.14-1.187-.437-2.262-1.395-.835-.745-1.4-1.665-1.564-1.946-.164-.282-.018-.434.124-.574.127-.127.281-.328.422-.492.14-.164.187-.281.281-.469.094-.187.047-.351-.023-.492-.07-.141-.633-1.523-.866-2.084-.227-.547-.46-.472-.633-.481-.164-.008-.351-.01-.539-.01a1.04 1.04 0 0 0-.751.351c-.258.281-.984.961-.984 2.344 0 1.383 1.008 2.719 1.148 2.906.141.187 1.984 3.028 4.805 4.244.671.291 1.196.464 1.604.594.674.214 1.287.184 1.772.112.541-.081 1.663-.679 1.898-1.336.234-.656.234-1.219.164-1.336-.07-.117-.258-.187-.539-.328Z"/>
            </svg>
          </span>
          <span>WhatsApp арқылы жазу</span>
        </button>
      </section>
    `);
    bindNext();
    on("profileWhatsappSupport", openWhatsAppSupport);
  }

  function renderSupport() {
    html(`
      <section class="screen">
        <div class="card">
          <p class="eyebrow">Қолдау</p>
          <h1>Қолдау қызметі</h1>
          <p class="muted">Сұрағыңызды жазыңыз. Команда жауап береді.</p>
        </div>
        <div class="card whatsapp-card">
          <div class="whatsapp-card-head">
            <span class="whatsapp-mark" aria-hidden="true">
              <svg viewBox="0 0 32 32" width="22" height="22" focusable="false" aria-hidden="true">
                <path fill="currentColor" d="M16.02 5.333c-5.9 0-10.687 4.787-10.687 10.687 0 1.88.493 3.713 1.427 5.333L5.333 26.667l5.46-1.4a10.66 10.66 0 0 0 5.227 1.36h.004c5.899 0 10.687-4.788 10.687-10.687 0-2.854-1.111-5.535-3.129-7.553a10.617 10.617 0 0 0-7.562-3.054Zm0 19.45h-.003a8.86 8.86 0 0 1-4.517-1.237l-.324-.192-3.24.829.866-3.16-.211-.336a8.86 8.86 0 0 1-1.358-4.717c0-4.9 3.987-8.887 8.89-8.887 2.373 0 4.604.926 6.281 2.606a8.829 8.829 0 0 1 2.605 6.288c0 4.9-3.987 8.806-8.989 8.806Zm5.124-6.624c-.281-.141-1.663-.821-1.92-.914-.257-.094-.444-.141-.631.141-.187.281-.722.914-.886 1.101-.164.187-.327.21-.609.07-.281-.14-1.187-.437-2.262-1.395-.835-.745-1.4-1.665-1.564-1.946-.164-.282-.018-.434.124-.574.127-.127.281-.328.422-.492.14-.164.187-.281.281-.469.094-.187.047-.351-.023-.492-.07-.141-.633-1.523-.866-2.084-.227-.547-.46-.472-.633-.481-.164-.008-.351-.01-.539-.01a1.04 1.04 0 0 0-.751.351c-.258.281-.984.961-.984 2.344 0 1.383 1.008 2.719 1.148 2.906.141.187 1.984 3.028 4.805 4.244.671.291 1.196.464 1.604.594.674.214 1.287.184 1.772.112.541-.081 1.663-.679 1.898-1.336.234-.656.234-1.219.164-1.336-.07-.117-.258-.187-.539-.328Z"/>
              </svg>
            </span>
            <div>
              <p class="eyebrow">WhatsApp</p>
              <h2>WhatsApp қолдау</h2>
              <p class="muted">Сұрағыңыз болса, WhatsApp арқылы қолдау қызметіне жазыңыз.</p>
            </div>
          </div>
          <button id="whatsappSupportBtn" class="gold-btn lg block" type="button"><span class="btn-label">WhatsApp арқылы жазу</span></button>
        </div>
        <form id="supportForm" class="form">
          <label class="field"><span>Хабарлама</span><textarea name="body" required placeholder="Хабарлама..."></textarea></label>
          <button class="gold-btn lg" type="submit"><span class="btn-label">Жіберу</span><span class="btn-spinner"></span></button>
        </form>
      </section>
    `);
    on("whatsappSupportBtn", openWhatsAppSupport);
    document.getElementById("supportForm").addEventListener("submit", async (event) => {
      event.preventDefault();
      const submitBtn = event.currentTarget.querySelector("button[type=submit]");
      if (buttonIsLoading(submitBtn)) return;
      setButtonLoading(submitBtn, true);
      try {
        const body = new FormData(event.currentTarget).get("body");
        const res = await api("/api/support", {
          method: "POST",
          body: JSON.stringify({ body }),
        });
        toast(res.message || "Хабарламаңыз әкімшіге жіберілді. Жауапты осы чаттан күтіңіз.", "success");
        setScreen("dashboard");
      } catch (error) {
        toast(error.message || "Хабарламаны жіберу мүмкін болмады. Кейінірек қайталап көріңіз.", "error");
      } finally {
        setButtonLoading(submitBtn, false);
      }
    });
  }

	  function bindNext() {
	    document.querySelectorAll("[data-next]").forEach((button) => {
	      button.addEventListener("click", () => {
	        if (button.dataset.next === "test") {
	          openTest();
	          return;
	        }
	        setScreen(button.dataset.next);
	      });
	    });
	  }

  async function refreshLevels() {
    try {
      const data = await api("/api/levels");
      state.levels = data.levels || [];
    } catch (_) {}
  }

  function emptyState(text, icon) {
    return `<div class="empty-state"><div class="icon">${esc(icon || "Ø")}</div><span>${esc(text || "Бос")}</span></div>`;
  }

  function skeletonRows(count) {
    return Array.from({ length: count })
      .map(
        () => `<div class="card">
          <div class="skeleton-row w-50"></div>
          <div class="skeleton-row w-90"></div>
          <div class="skeleton-row w-70"></div>
        </div>`,
      )
      .join("");
  }

	  /* ===========================================================
	     BROWSER ADMIN
	     =========================================================== */

	  const ADMIN_POLL_MS = 2000;

	  function stopAdminPolling() {
	    const polling = state.adminPolling;
	    if (polling && polling.timer) {
	      clearInterval(polling.timer);
	    }
	    state.adminPolling = { screen: "", timer: null, loading: false, lastWarningAt: polling ? polling.lastWarningAt || 0 : 0 };
	  }

	  function startAdminPolling(screen, loader) {
	    if (!screen || typeof loader !== "function") return;
	    const current = state.adminPolling || {};
	    if (current.timer && current.screen === screen) return;
	    stopAdminPolling();
	    const polling = state.adminPolling;
	    polling.screen = screen;
	    polling.timer = setInterval(async () => {
	      if (state.mode !== "admin" || !state.admin || state.adminScreen !== screen) {
	        stopAdminPolling();
	        return;
	      }
	      if (polling.loading) return;
	      polling.loading = true;
	      try {
	        await loader({ silent: true });
	      } catch (error) {
	        adminPollWarning(error);
	      } finally {
	        polling.loading = false;
	      }
	    }, ADMIN_POLL_MS);
	  }

	  function adminPollWarning(error) {
	    const polling = state.adminPolling || {};
	    const now = Date.now();
	    if (now - (polling.lastWarningAt || 0) < 12000) return;
	    polling.lastWarningAt = now;
	    state.adminPolling = polling;
	    toast((error && error.message) || "Авто жаңарту уақытша сәтсіз", "warn");
	  }

	  async function bootBrowserAdmin() {
    try {
      const me = await api("/api/browser-auth/me");
      state.admin = me.admin;
      showAdminAuthenticated();
      renderAdminNav();
      renderAdmin();
    } catch (_) {
      showAdminLogin();
    }
  }

	  function showAdminLogin() {
	    stopAdminPolling();
	    if (els.adminApp) {
	      els.adminApp.classList.add("hidden");
      els.adminApp.setAttribute("aria-hidden", "true");
    }
    if (els.adminAuth) {
      els.adminAuth.classList.remove("hidden");
      els.adminAuth.setAttribute("aria-hidden", "false");
    }
    // dev hint
    if (els.adminLoginHint) {
      const dev =
        location.hostname === "localhost" ||
        location.hostname === "127.0.0.1" ||
        location.hostname.endsWith(".local");
      els.adminLoginHint.classList.toggle("hidden", !dev);
    }
    if (els.adminPassword) {
      setTimeout(() => els.adminPassword.focus(), 80);
    }
    if (els.adminLoginForm) {
      els.adminLoginForm.onsubmit = async (event) => {
        event.preventDefault();
        if (els.adminLoginError) els.adminLoginError.classList.add("hidden");
        const btn = els.adminLoginSubmit;
        if (buttonIsLoading(btn)) return;
        setButtonLoading(btn, true);
        try {
          const password = new FormData(event.currentTarget).get("password");
          const res = await api("/api/browser-auth/login", {
            method: "POST",
            body: JSON.stringify({ password }),
          });
          state.admin = res.admin;
          showAdminAuthenticated();
          renderAdminNav();
          renderAdmin();
        } catch (error) {
          if (els.adminLoginError) {
            els.adminLoginError.textContent = error.message || "Қате құпия сөз";
            els.adminLoginError.classList.remove("hidden");
          }
        } finally {
          setButtonLoading(btn, false);
        }
      };
    }
  }

  function showAdminAuthenticated() {
    if (els.adminAuth) {
      els.adminAuth.classList.add("hidden");
      els.adminAuth.setAttribute("aria-hidden", "true");
    }
    if (els.adminApp) {
      els.adminApp.classList.remove("hidden");
      els.adminApp.setAttribute("aria-hidden", "false");
    }
    if (els.adminWho && state.admin) {
      els.adminWho.textContent = `${state.admin.name || "Admin"} · ${state.admin.role || ""}`;
    }
  }

	  async function handleAdminLogout() {
	    try {
	      await api("/api/browser-auth/logout", { method: "POST", body: "{}" });
	    } catch (_) {}
	    stopAdminPolling();
	    state.admin = null;
	    showAdminLogin();
	  }

  const adminScreens = [
    ["dashboard", "Басты бет"],
	    ["users", "Қолданушылар"],
	    ["payments", "Төлемдер"],
	    ["subscriptions", "Жазылымдар"],
	    ["tariffs", "Тарифтер"],
	    ["levels", "Деңгейлер"],
    ["lessons", "Сабақтар"],
    ["freeLessons", "Тегін сабақтар"],
    ["books", "Кітаптар"],
    ["premiumCourses", "Premium курстар"],
    ["tests", "Тесттер"],
    ["assignments", "Тапсырмалар"],
	    ["referrals", "Дос шақыру"],
    ["coins", "ZHENIS Coin"],
    ["channels", "Каналдар"],
    ["streams", "Эфирлер"],
    ["broadcast", "Хабарлама"],
    ["analytics", "Аналитика"],
    ["settings", "Баптаулар"],
    ["audit", "Аудит журналы"],
  ];

  function renderAdminNav() {
    if (!els.adminNav) return;
    els.adminNav.innerHTML = adminScreens
      .map(
        ([key, label]) =>
          `<button class="tool-btn ${state.adminScreen === key ? "active" : ""}" data-admin-screen="${esc(key)}" type="button">${esc(label)}</button>`,
      )
      .join("");
    els.adminNav.querySelectorAll("[data-admin-screen]").forEach((button) =>
      button.addEventListener("click", () => {
        state.adminScreen = button.dataset.adminScreen;
        renderAdminNav();
        renderAdmin();
        if (els.adminApp) els.adminApp.classList.remove("nav-open");
      }),
    );
  }

	  async function renderAdmin() {
	    stopAdminPolling();
	    if (els.adminTitle) {
      const meta = adminScreens.find(([key]) => key === state.adminScreen);
      els.adminTitle.textContent = meta ? meta[1] : "Басты бет";
    }
    const screen = state.adminScreen;
    if (!els.adminContent) return;

    els.adminContent.innerHTML = `<div class="grid four">${Array.from({ length: 4 })
      .map(() => `<div class="metric skeleton"></div>`)
      .join("")}</div>`;

    try {
      if (screen === "dashboard" || screen === "analytics") return await renderAdminDashboard();
      if (screen === "users") return await renderAdminUsers();
      if (screen === "payments") return await renderAdminPayments();
	      if (screen === "subscriptions")
	        return await renderAdminTable(
	          "/api/admin/subscriptions",
	          "subscriptions",
	          ["id", "user_id", "tariff_code", "status", "expires_at"],
	          "Жазылымдар",
	        );
	      if (screen === "tariffs") return await renderAdminTariffs();
      if (screen === "levels") return await renderAdminLevels();
      if (screen === "lessons") return await renderAdminLessons();
      if (screen === "freeLessons") return await renderAdminFreeLessons();
      if (screen === "books") return await renderAdminBooks();
      if (screen === "premiumCourses") return await renderAdminPremiumCourses();
      if (screen === "tests") return await renderAdminTests();
      if (screen === "assignments") return await renderAdminItems("/api/admin/assignments/submissions", "Тапсырма жауаптары");
	      if (screen === "referrals") return await renderAdminItems("/api/admin/referrals", "Дос шақыру");
      if (screen === "coins") return await renderAdminItems("/api/admin/coins", "ZHENIS Coin");
      if (screen === "channels") return await renderAdminChannels();
      if (screen === "streams")
        return await renderAdminTable(
          "/api/admin/streams",
          "streams",
          ["id", "title", "starts_at", "tariff_requirement", "status"],
          "Эфирлер",
        );
      if (screen === "broadcast") return await renderAdminBroadcast();
      if (screen === "settings") return await renderAdminSettings();
      if (screen === "audit")
        return await renderAdminTable(
          "/api/admin/audit",
          "actions",
          ["id", "role", "action", "entity_type", "created_at"],
          "Аудит журналы",
        );
    } catch (error) {
      els.adminContent.innerHTML = `<div class="error-card"><p class="eyebrow">Қате</p><h2>Қате</h2><p>${esc(error.message)}</p></div>`;
    }
  }

  async function renderAdminDashboard() {
    const data = await api("/api/admin/stats");
    const stats = data.stats || {};
    const ordered = [
      "users_total",
      "active_subscriptions",
      "pending_payments",
      "uploaded_receipts",
      "approved_payments",
      "monthly_revenue_kzt",
      "lessons_count",
      "tests_count",
      "referrals_paid",
      "expired_subscriptions",
    ];
    const metricLabels = {
      users_total: "Барлық қолданушылар",
      active_subscriptions: "Белсенді жазылымдар",
      pending_payments: "Күтудегі төлемдер",
      uploaded_receipts: "Тексерілетін чектер",
      approved_payments: "Қабылданған төлемдер",
      monthly_revenue_kzt: "Айлық табыс",
      lessons_count: "Сабақтар",
      tests_count: "Тесттер",
      referrals_paid: "Төленген рефералдар",
      expired_subscriptions: "Мерзімі өткендер",
    };
    const metrics = ordered
      .filter((key) => key in stats)
      .map((key) => `<div class="metric"><p class="eyebrow">${esc(metricLabels[key])}</p><strong>${esc(money(stats[key]))}</strong><span class="muted">тірі</span></div>`);
    els.adminContent.innerHTML = `
      <div class="admin-grid">${metrics.join("")}</div>
      <div class="card">
        <div class="section-head">
          <div><p class="eyebrow">Басқару орталығы</p><h2>ZHENIS ORDA Command Center</h2></div>
        </div>
        <p class="muted">Mini App, төлемдер, контент және каналдар бойынша толық бақылау.</p>
      </div>
    `;
  }

	  async function renderAdminUsers() {
	    const users = await fetchAdminUsers();
	    els.adminContent.innerHTML = `
	      <div class="card">
	        <div class="admin-section-head">
	          <div><p class="eyebrow">Қолданушылар</p><h2>Қолданушылар</h2></div>
	          <div class="admin-toolbar">
	            <input id="userSearch" placeholder="Аты немесе username бойынша іздеу" value="${esc(state.adminUserSearch)}" />
	          </div>
	        </div>
	        ${adminUsersTableHtml(users)}
	      </div>
	    `;
	    const reloadUsers = debounce(() => {
	      loadAdminUsersTable().catch((error) => toast(error.message, "error"));
	    }, 280);
	    on("userSearch", (event) => {
	      state.adminUserSearch = event.target.value;
	      reloadUsers();
	    }, "input");
	    delegate(els.adminContent, "[data-user-access]", "click", (event, target) => {
	      const user = (state.adminUsers || []).find((item) => item.id === target.dataset.userAccess) || { id: target.dataset.userAccess };
	      openUserAccessModal(user);
	    });
	    startAdminPolling("users", loadAdminUsersTable);
	  }

	  async function fetchAdminUsers() {
	    if (state.adminUsersLoading) return state.adminUsers || [];
	    state.adminUsersLoading = true;
	    const q = state.adminUserSearch || "";
	    try {
	      const data = await api(`/api/admin/users?q=${encodeURIComponent(q)}`);
	      state.adminUsers = data.users || [];
	      return state.adminUsers;
	    } finally {
	      state.adminUsersLoading = false;
	    }
	  }

	  async function loadAdminUsersTable() {
	    const users = await fetchAdminUsers();
	    const table = els.adminContent && els.adminContent.querySelector("[data-admin-users-table]");
	    if (table) {
	      table.replaceWith(htmlToNode(adminUsersTableHtml(users)));
	    }
	    return users;
	  }

	  function adminUsersTableHtml(users) {
	    if (!users.length) return `<div data-admin-users-table>${emptyState("Қолданушылар табылмады")}</div>`;
	    return `<div class="table-wrap" data-admin-users-table><table class="admin-users-table">
	      <thead><tr><th>ID</th><th>Telegram</th><th>Қолданушы</th><th>Деңгей</th><th>Coin</th><th>Қолжетімділік</th><th>Әрекет</th></tr></thead>
	      <tbody>${users
	        .map((user) => {
	          const blocked = Boolean(user.access_closed);
	          return `<tr>
	            <td><code class="mono-id">${esc(shortId(user.id))}</code></td>
	            <td><code class="mono-id">${esc(user.telegram_id || "—")}</code></td>
	            <td>
	              <strong>${esc(user.first_name || "—")}</strong>
	              <div class="muted small">${esc(user.username ? `@${user.username}` : "")}</div>
	              ${blocked && user.blocked_reason ? `<div class="muted small wrap-text">Себеп: ${esc(user.blocked_reason)}</div>` : ""}
	            </td>
	            <td>${esc(user.current_level || 0)}</td>
	            <td>${money(user.coin_balance || 0)}</td>
	            <td>${statusBadge(blocked ? "blocked" : "active")}</td>
	            <td><button class="${blocked ? "gold-btn" : "danger-btn"} icon-mini" data-user-access="${esc(user.id)}" type="button">${blocked ? "Бұғаттан шығару" : "Бұғаттау"}</button></td>
	          </tr>`;
	        })
	        .join("")}</tbody>
	    </table></div>`;
	  }

	  function openUserAccessModal(user) {
	    const blocked = Boolean(user.access_closed);
	    const displayName = compact(user.first_name) || compact(user.username) || shortId(user.id);
	    const shell = openModalShell("Қолжетімділік", `
	      <div class="access-modal">
	        <div class="admin-section-head">
	          <div>
	            <p class="eyebrow">${esc(blocked ? "Бұғатталған" : "Белсенді")}</p>
	            <h2>${esc(displayName)}</h2>
	          </div>
	          ${statusBadge(blocked ? "blocked" : "active")}
	        </div>
	        <div class="access-detail-grid">
	          <div><span>Telegram ID</span><code>${esc(user.telegram_id || "—")}</code></div>
	          <div><span>Username</span><code>${esc(user.username ? `@${user.username}` : "—")}</code></div>
	          <div><span>Қазіргі статус</span><strong>${blocked ? "Қолжетімділік жабық" : "Қолжетімділік ашық"}</strong></div>
	          <div><span>Бұғаттау себебі</span><strong>${esc(blocked ? user.blocked_reason || "—" : "—")}</strong></div>
	          <div><span>Бұғатталған уақыты</span><strong>${blocked && user.blocked_at ? formatDateTime(user.blocked_at) : "—"}</strong></div>
	          <div><span>Бұғаттаған admin</span><code>${esc(blocked && user.blocked_by_admin_id ? user.blocked_by_admin_id : "—")}</code></div>
	        </div>
	        <form id="userAccessForm" class="form">
	          <label class="field">
	            <span>${blocked ? "Қалпына келтіру пікірі" : "Бұғаттау себебі"}</span>
	            <textarea name="reason" ${blocked ? "" : "required"} placeholder="${blocked ? "Қолжетімділік қайта ашылды" : "Себебін жазыңыз"}"></textarea>
	          </label>
	          <div class="action-row end">
	            <button class="ghost-btn" data-close-modal type="button">Жабу</button>
	            <button class="${blocked ? "gold-btn" : "danger-btn"}" type="submit">
	              <span class="btn-label">${blocked ? "Бұғаттан шығару" : "Бұғаттау"}</span><span class="btn-spinner"></span>
	            </button>
	          </div>
	        </form>
	      </div>
	    `);
	    shell.body.querySelector("[data-close-modal]")?.addEventListener("click", () => shell.close());
	    shell.body.querySelector("#userAccessForm").addEventListener("submit", async (event) => {
	      event.preventDefault();
	      const form = event.currentTarget;
	      const submitBtn = form.querySelector("button[type=submit]");
	      const reason = compact(new FormData(form).get("reason"));
	      if (!blocked && !reason) {
	        toast("Бұғаттау себебін жазыңыз", "error");
	        return;
	      }
	      if (buttonIsLoading(submitBtn)) return;
	      setButtonLoading(submitBtn, true);
	      setModalBusy(shell, true);
	      try {
	        const res = await api(`/api/admin/users/${user.id}/${blocked ? "unblock" : "block"}`, {
	          method: "POST",
	          body: JSON.stringify({ reason }),
	        });
	        toast(blocked ? "Қолжетімділік ашылды" : "Қолданушы бұғатталды", "success");
	        if (res.warnings && res.warnings.length) toast(res.warnings.join("; "), "warn");
	        shell.close();
	        await loadAdminUsersTable();
	      } catch (error) {
	        toast(error.message || "Қолжетімділікті өзгерту мүмкін болмады", "error");
	        setModalBusy(shell, false);
	        setButtonLoading(submitBtn, false);
	      }
	    });
	  }

	  async function renderAdminTable(url, key, columns, title) {
	    const data = await api(url);
	    const rows = data[key] || data.items || [];
	    els.adminContent.innerHTML = `
      <div class="card">
        <div class="admin-section-head">
          <div><p class="eyebrow">${esc(title || key)}</p><h2>${esc(title || key)}</h2></div>
        </div>
        ${tableHtml(columns, rows)}
      </div>
	    `;
	  }

	  async function renderAdminTariffs() {
	    const data = await api("/api/admin/tariffs");
	    const tariffs = data.tariffs || [];
	    const rows = tariffs.length
	      ? `<div class="table-wrap"><table>
	          <thead><tr><th>Сурет</th><th>Тариф</th><th>Баға</th><th>Реті</th><th>Статус</th><th>Әрекет</th></tr></thead>
	          <tbody>${tariffs
	            .map((tariff) => {
	              const image = visibleTariffImage(tariff);
	              return `<tr>
	                <td>${image ? `<img class="tariff-admin-thumb" src="${esc(image)}" alt="${esc(tariff.title || tariff.code)}" loading="lazy" />` : "—"}</td>
	                <td><strong>${esc(tariff.title || tariff.code)}</strong><div class="muted small">${esc(tariff.code)} · ${esc(shortId(tariff.id))}</div><div class="muted small">${esc(tariff.short_description_kk || "")}</div></td>
	                <td>${money(tariff.price_kzt)} ₸</td>
	                <td>${esc(tariff.sort_order || 0)}</td>
	                <td>${statusBadge(tariff.is_active ? "active" : "inactive")}</td>
	                <td><div class="action-row">
	                  <button class="ghost-btn" data-edit-tariff="${esc(tariff.id)}" type="button">Өзгерту</button>
	                  <button class="danger-btn" data-archive-tariff="${esc(tariff.id)}" type="button">Белсенді емес</button>
	                </div></td>
	              </tr>`;
	            })
	            .join("")}</tbody></table></div>`
	      : emptyState("Тарифтер табылмады");
	    els.adminContent.innerHTML = `
	      <div class="card">
	        <div class="admin-section-head">
	          <div><p class="eyebrow">Тарифтер</p><h2>Тарифтер</h2></div>
	          <div class="admin-toolbar"><button class="gold-btn" id="addTariff" type="button">+ Тариф қосу</button></div>
	        </div>
	        ${rows}
	      </div>
	    `;
	    on("addTariff", () => openTariffModal(null, tariffs));
	    delegate(els.adminContent, "[data-edit-tariff]", "click", (event, target) => {
	      const tariff = tariffs.find((item) => item.id === target.dataset.editTariff);
	      if (tariff) openTariffModal(tariff, tariffs);
	    });
	    delegate(els.adminContent, "[data-archive-tariff]", "click", async (event, target) => {
	      const tariff = tariffs.find((item) => item.id === target.dataset.archiveTariff);
	      const ok = await confirmAction({
	        title: "Тарифті белсенді емес ету",
	        body: `<p class="muted">${esc(tariff ? tariff.title || tariff.code : "Бұл тариф")} Mini App-та көрінбейді. Жалғастырасыз ба?</p>`,
	        confirmLabel: "Белсенді емес",
	        action: () => api(`/api/admin/tariffs/${target.dataset.archiveTariff}`, { method: "DELETE" }),
	        successMessage: "Тариф белсенді емес күйге ауысты",
	        errorMessage: "Тарифті өзгерту мүмкін болмады",
	      });
	      if (!ok) return;
	      renderAdminTariffs();
	    });
	  }

	  function openTariffModal(tariff, tariffs) {
	    const isEdit = Boolean(tariff && tariff.id);
	    const image = visibleTariffImage(tariff);
	    const benefits = (tariff && tariff.features ? tariff.features : []).join("\n");
	    const nextOrder = Math.max(0, ...tariffs.map((item) => Number(item.sort_order) || 0)) + 1;
	    const shell = openModalShell(isEdit ? "Тарифті өзгерту" : "Тариф қосу", `
	      <form id="tariffModalForm" class="form">
	        <div class="grid three">
	          <label class="field"><span>Код / slug</span><input name="code" required placeholder="BASIC" value="${esc((tariff && tariff.code) || "")}" /></label>
	          <label class="field"><span>Атауы</span><input name="title" required placeholder="BASIC" value="${esc((tariff && tariff.title) || "")}" /></label>
	          <label class="field"><span>Баға, KZT</span><input name="price_kzt" type="number" min="1" required value="${esc((tariff && tariff.price_kzt) || "")}" /></label>
	        </div>
	        <label class="field"><span>Қысқа сипаттама</span><input name="short_description_kk" value="${esc((tariff && tariff.short_description_kk) || "")}" /></label>
	        <label class="field"><span>Толық сипаттама</span><textarea name="full_description_kk">${esc((tariff && tariff.full_description_kk) || "")}</textarea></label>
	        <label class="field"><span>Артықшылықтар</span><textarea name="features" placeholder="Әр жолға бір артықшылық">${esc(benefits)}</textarea></label>
	        <div class="grid two">
	          <label class="field"><span>Сурет URL</span><input name="image_url" placeholder="https://" value="${esc((tariff && tariff.image_url) || "")}" /></label>
	          <label class="field"><span>Реті</span><input name="sort_order" type="number" min="1" value="${esc((tariff && tariff.sort_order) || nextOrder)}" /></label>
	        </div>
	        <label class="upload-drop tariff-upload">
	          <input name="image_upload" type="file" accept=".jpg,.jpeg,.png,.webp,image/*" />
	          <span class="upload-title">Сурет жүктеу</span>
	          <small>JPG, PNG немесе WEBP</small>
	          <strong id="tariffUploadName">Файл таңдалмады</strong>
	        </label>
	        <input type="hidden" name="image_file_path" value="${esc((tariff && tariff.image_file_path) || "")}" />
	        <input type="hidden" name="image_source" value="${esc((tariff && tariff.image_source) || "none")}" />
	        <div id="tariffImagePreview" class="tariff-image-preview">${image ? `<img src="${esc(image)}" alt="${esc((tariff && tariff.title) || "Тариф")}" />` : ""}</div>
	        <label class="switch-field"><input name="is_active" type="checkbox" ${!tariff || tariff.is_active ? "checked" : ""} /><span>Белсенді</span></label>
	        <div class="action-row end">
	          <button class="ghost-btn" data-close-modal type="button">Болдырмау</button>
	          <button class="gold-btn" type="submit"><span class="btn-label">Сақтау</span><span class="btn-spinner"></span></button>
	        </div>
	      </form>
	    `);
	    const form = shell.body.querySelector("form");
	    const upload = form.querySelector("input[name=image_upload]");
	    const uploadName = shell.body.querySelector("#tariffUploadName");
	    const preview = shell.body.querySelector("#tariffImagePreview");
	    shell.body.querySelector("[data-close-modal]").addEventListener("click", shell.close);
	    upload.addEventListener("change", async () => {
	      const file = upload.files && upload.files[0];
	      uploadName.textContent = file ? file.name : "Файл таңдалмады";
	      if (!file) return;
	      const fd = new FormData();
	      fd.append("image", file);
	      try {
	        const res = await api("/api/admin/tariffs/image", { method: "POST", body: fd });
	        form.elements.image_file_path.value = res.image_file_path || "";
	        form.elements.image_source.value = "uploaded";
	        preview.innerHTML = res.image_file_path ? `<img src="${esc(res.image_file_path)}" alt="Тариф суреті" />` : "";
	        toast("Сурет жүктелді", "success");
	      } catch (error) {
	        toast(error.message || "Сурет жүктеу мүмкін болмады", "error");
	      }
	    });
	    form.addEventListener("submit", async (event) => {
	      event.preventDefault();
	      const btn = form.querySelector("button[type=submit]");
	      const fd = new FormData(form);
	      const imageFilePath = compact(fd.get("image_file_path"));
	      const imageURL = compact(fd.get("image_url"));
	      const payload = {
	        code: compact(fd.get("code")),
	        title: compact(fd.get("title")),
	        price_kzt: Number(fd.get("price_kzt") || 0),
	        short_description_kk: compact(fd.get("short_description_kk")),
	        full_description_kk: compact(fd.get("full_description_kk")),
	        features: compact(fd.get("features")).split("\n").map((item) => item.trim()).filter(Boolean),
	        image_url: imageURL,
	        image_file_path: imageFilePath,
	        image_source: imageFilePath ? "uploaded" : imageURL ? "url" : "none",
	        sort_order: Number(fd.get("sort_order") || 0),
	        is_active: fd.get("is_active") === "on",
	      };
	      if (!payload.code || !payload.title || payload.price_kzt <= 0) {
	        toast("Код, атау және баға міндетті", "error");
	        return;
	      }
	      if (buttonIsLoading(btn)) return;
	      setButtonLoading(btn, true);
	      setModalBusy(shell, true);
	      try {
	        await api(isEdit ? `/api/admin/tariffs/${tariff.id}` : "/api/admin/tariffs", {
	          method: isEdit ? "PATCH" : "POST",
	          body: JSON.stringify(payload),
	        });
	        shell.close();
	        toast("Тариф сақталды", "success");
	        renderAdminTariffs();
	      } catch (error) {
	        toast(error.message || "Тарифті сақтау мүмкін болмады", "error");
	      } finally {
	        setModalBusy(shell, false);
	        setButtonLoading(btn, false);
	      }
		    });
		  }

	  async function renderAdminBooks() {
	    const data = await api("/api/admin/books");
	    const books = data.books || [];
	    els.adminContent.innerHTML = `
	      <div class="card">
	        <div class="admin-section-head">
	          <div><p class="eyebrow">Сату</p><h2>Кітаптар</h2></div>
	          <div class="admin-toolbar"><button class="gold-btn" id="addBook" type="button">+ Кітап қосу</button></div>
	        </div>
	        ${bookTableHtml(books)}
	      </div>
	    `;
	    on("addBook", () => openBookModal(null, books));
	    delegate(els.adminContent, "[data-edit-book]", "click", (event, target) => {
	      const book = books.find((item) => item.id === target.dataset.editBook);
	      if (book) openBookModal(book, books);
	    });
	    delegate(els.adminContent, "[data-archive-book]", "click", async (event, target) => {
	      const book = books.find((item) => item.id === target.dataset.archiveBook);
	      const ok = await confirmAction({
	        title: "Кітапты белсенді емес ету",
	        body: `<p class="muted">${esc(book ? book.title : "Бұл кітап")} клиент жағында көрінбейді. Жалғастырасыз ба?</p>`,
	        confirmLabel: "Белсенді емес",
	        action: () => api(`/api/admin/books/${target.dataset.archiveBook}`, { method: "DELETE" }),
	        successMessage: "Кітап белсенді емес күйге ауысты",
	        errorMessage: "Кітапты өзгерту мүмкін болмады",
	      });
	      if (!ok) return;
	      renderAdminBooks();
	    });
	  }

	  function bookTableHtml(books) {
	    if (!books.length) return emptyState("Кітаптар табылмады");
	    return `<div class="table-wrap"><table>
	      <thead><tr><th>Сурет</th><th>Кітап</th><th>Баға</th><th>Реті</th><th>Статус</th><th>Әрекет</th></tr></thead>
	      <tbody>${books
	        .map((book) => {
	          const image = visibleBookImage(book);
	          return `<tr>
	            <td>${image ? `<img class="tariff-admin-thumb" src="${esc(image)}" alt="${esc(book.title)}" loading="lazy" />` : "—"}</td>
	            <td><strong>${esc(book.title)}</strong><div class="muted small">${esc(shortId(book.id))}</div><div class="muted small">${esc(shortText(book.description, 110))}</div></td>
	            <td>${money(book.price_kzt)} ₸</td>
	            <td>${esc(book.sort_order || 0)}</td>
	            <td>${statusBadge(book.is_active ? "active" : "inactive")}</td>
	            <td><div class="action-row">
	              <button class="ghost-btn" data-edit-book="${esc(book.id)}" type="button">Өзгерту</button>
	              <button class="danger-btn" data-archive-book="${esc(book.id)}" type="button">Белсенді емес</button>
	            </div></td>
	          </tr>`;
	        })
	        .join("")}</tbody></table></div>`;
	  }

	  function openBookModal(book, books) {
	    const isEdit = Boolean(book && book.id);
	    const image = visibleBookImage(book);
	    const nextOrder = Math.max(0, ...books.map((item) => Number(item.sort_order) || 0)) + 1;
	    const shell = openModalShell(isEdit ? "Кітапты өзгерту" : "Кітап қосу", `
	      <form id="bookModalForm" class="form">
	        <div class="grid two">
	          <label class="field"><span>Атауы</span><input name="title" required value="${esc((book && book.title) || "")}" /></label>
	          <label class="field"><span>Баға, KZT</span><input name="price_kzt" type="number" min="1" required value="${esc((book && book.price_kzt) || "")}" /></label>
	        </div>
	        <label class="field"><span>Сипаттама</span><textarea name="description" required>${esc((book && book.description) || "")}</textarea></label>
	        <div class="grid two">
	          <label class="field"><span>Сурет URL</span><input name="image_url" placeholder="https://" value="${esc((book && book.image_url) || "")}" /></label>
	          <label class="field"><span>Реті</span><input name="sort_order" type="number" min="1" value="${esc((book && book.sort_order) || nextOrder)}" /></label>
	        </div>
	        <label class="upload-drop book-upload">
	          <input name="image_upload" type="file" accept=".jpg,.jpeg,.png,.webp,image/*" />
	          <span class="upload-title">Кітап суретін жүктеу</span>
	          <small>JPG, PNG немесе WEBP, 5 MB дейін</small>
	          <strong id="bookUploadName">Файл таңдалмады</strong>
	        </label>
	        <input type="hidden" name="image_file_path" value="${esc((book && book.image_file_path) || "")}" />
	        <input type="hidden" name="image_source" value="${esc((book && book.image_source) || "none")}" />
	        <div id="bookImagePreview" class="tariff-image-preview">${image ? `<img src="${esc(image)}" alt="${esc((book && book.title) || "Кітап")}" />` : ""}</div>
	        <label class="switch-field"><input name="is_active" type="checkbox" ${!book || book.is_active ? "checked" : ""} /><span>Белсенді</span></label>
	        <div class="action-row end">
	          <button class="ghost-btn" data-close-modal type="button">Болдырмау</button>
	          <button class="gold-btn" type="submit"><span class="btn-label">Сақтау</span><span class="btn-spinner"></span></button>
	        </div>
	      </form>
	    `);
	    const form = shell.body.querySelector("form");
	    const upload = form.querySelector("input[name=image_upload]");
	    const uploadName = shell.body.querySelector("#bookUploadName");
	    const preview = shell.body.querySelector("#bookImagePreview");
	    shell.body.querySelector("[data-close-modal]").addEventListener("click", shell.close);
	    upload.addEventListener("change", async () => {
	      const file = upload.files && upload.files[0];
	      uploadName.textContent = file ? file.name : "Файл таңдалмады";
	      if (!file) return;
	      const fd = new FormData();
	      fd.append("image", file);
	      try {
	        const res = await api("/api/admin/books/upload-image", { method: "POST", body: fd });
	        form.elements.image_file_path.value = res.image_file_path || "";
	        form.elements.image_source.value = "uploaded";
	        form.elements.image_url.value = "";
	        preview.innerHTML = res.image_file_path ? `<img src="${esc(res.image_file_path)}" alt="Кітап суреті" />` : "";
	        toast("Сурет жүктелді", "success");
	      } catch (error) {
	        toast(error.message || "Сурет жүктеу мүмкін болмады", "error");
	      }
	    });
	    form.addEventListener("submit", async (event) => {
	      event.preventDefault();
	      const btn = form.querySelector("button[type=submit]");
	      const fd = new FormData(form);
	      const imageURL = compact(fd.get("image_url"));
	      const existingImageURL = compact(book && book.image_url);
	      const preferURL = imageURL && imageURL !== existingImageURL;
	      const imageFilePath = preferURL ? "" : compact(fd.get("image_file_path"));
	      const payload = {
	        title: compact(fd.get("title")),
	        description: compact(fd.get("description")),
	        price_kzt: Number(fd.get("price_kzt") || 0),
	        image_url: imageURL,
	        image_file_path: imageFilePath,
	        image_source: imageFilePath ? "uploaded" : imageURL ? "url" : "none",
	        sort_order: Number(fd.get("sort_order") || 0),
	        is_active: fd.get("is_active") === "on",
	      };
	      if (!payload.title || !payload.description || payload.price_kzt <= 0) {
	        toast("Атауы, сипаттама және баға міндетті", "error");
	        return;
	      }
	      if (!payload.image_url && !payload.image_file_path) {
	        toast("Кітап суретін URL арқылы немесе файл жүктеп қосыңыз", "error");
	        return;
	      }
	      if (buttonIsLoading(btn)) return;
	      setButtonLoading(btn, true);
	      setModalBusy(shell, true);
	      try {
	        await api(isEdit ? `/api/admin/books/${book.id}` : "/api/admin/books", {
	          method: isEdit ? "PATCH" : "POST",
	          body: JSON.stringify(payload),
	        });
	        shell.close();
	        toast("Кітап сақталды", "success");
	        renderAdminBooks();
	      } catch (error) {
	        toast(error.message || "Кітапты сақтау мүмкін болмады", "error");
	      } finally {
	        setModalBusy(shell, false);
	        setButtonLoading(btn, false);
	      }
	    });
	  }

	  async function renderAdminPremiumCourses() {
	    const data = await api("/api/admin/premium-courses");
	    const courses = data.premium_courses || [];
	    els.adminContent.innerHTML = `
	      <div class="card">
	        <div class="admin-section-head">
	          <div><p class="eyebrow">Premium курстар</p><h2>Premium курстар</h2></div>
	          <div class="admin-toolbar"><button class="gold-btn" id="addPremiumCourse" type="button">+ Premium курс қосу</button></div>
	        </div>
	        ${premiumCourseAdminTableHtml(courses)}
	      </div>
	    `;
	    on("addPremiumCourse", () => openPremiumCourseModal(null, courses));
	    delegate(els.adminContent, "[data-edit-premium-course]", "click", (event, target) => {
	      const course = courses.find((item) => item.id === target.dataset.editPremiumCourse);
	      if (course) openPremiumCourseModal(course, courses);
	    });
	    delegate(els.adminContent, "[data-archive-premium-course]", "click", async (event, target) => {
	      const course = courses.find((item) => item.id === target.dataset.archivePremiumCourse);
	      const ok = await confirmAction({
	        title: "Premium курсты архивтеу",
	        body: `<p class="muted">${esc(course ? course.title : "Бұл курс")} Mini App-та көрінбейді. Жалғастырасыз ба?</p>`,
	        confirmLabel: "Архивтеу",
	        action: () => api(`/api/admin/premium-courses/${target.dataset.archivePremiumCourse}`, { method: "DELETE" }),
	        successMessage: "Premium курс архивке жіберілді",
	        errorMessage: "Premium курсты архивтеу мүмкін болмады",
	      });
	      if (ok) renderAdminPremiumCourses();
	    });
	  }

	  function premiumCourseAdminTableHtml(courses) {
	    if (!courses.length) return emptyState("Premium курстар табылмады");
	    return `<div class="table-wrap"><table>
	      <thead><tr><th>Сурет</th><th>Курс</th><th>Баға</th><th>Telegram</th><th>Доступ</th><th>Төлем</th><th>Статус</th><th>Әрекет</th></tr></thead>
	      <tbody>${courses
	        .map((course) => {
	          const image = visiblePremiumCourseImage(course);
	          const stats = course.stats || {};
	          return `<tr>
	            <td>${image ? `<img class="tariff-admin-thumb" src="${esc(image)}" alt="${esc(course.title)}" loading="lazy" />` : "—"}</td>
	            <td><strong>${esc(course.title)}</strong><div class="muted small">${esc(course.slug)} · ${esc(shortId(course.id))}</div><div class="muted small">${esc(shortText(course.description, 110))}</div></td>
	            <td>${money(course.price_kzt)} ₸</td>
	            <td>${course.telegram_configured ? statusBadge("active") : statusBadge("inactive")}<div class="muted small">${esc(course.invite_link_type || "manual")}</div></td>
	            <td>${esc(stats.active_access_count || 0)} active<div class="muted small">${esc(stats.revoked_access_count || 0)} revoked</div></td>
	            <td>${esc(stats.payment_count || 0)}</td>
	            <td>${statusBadge(course.status || "inactive")}</td>
	            <td><div class="action-row">
	              <button class="ghost-btn" data-edit-premium-course="${esc(course.id)}" type="button">Өзгерту</button>
	              <button class="danger-btn" data-archive-premium-course="${esc(course.id)}" type="button">Архивтеу</button>
	            </div></td>
	          </tr>`;
	        })
	        .join("")}</tbody></table></div>`;
	  }

	  function openPremiumCourseModal(course, courses) {
	    const isEdit = Boolean(course && course.id);
	    const image = visiblePremiumCourseImage(course);
	    const nextOrder = Math.max(0, ...courses.map((item) => Number(item.sort_order) || 0)) + 1;
	    const duration = (course && course.default_access_duration_type) || "lifetime";
	    const inviteType = (course && course.invite_link_type) || "manual";
	    const shell = openModalShell(isEdit ? "Premium курсты өзгерту" : "Premium курс қосу", `
	      <form id="premiumCourseModalForm" class="form">
	        <div class="grid three">
	          <label class="field"><span>Slug</span><input name="slug" required placeholder="altyn-formula" value="${esc((course && course.slug) || "")}" /></label>
	          <label class="field"><span>Курс атауы</span><input name="title" required value="${esc((course && course.title) || "")}" /></label>
	          <label class="field"><span>Бағасы</span><input name="price_kzt" type="number" min="1" required value="${esc((course && course.price_kzt) || "")}" /></label>
	        </div>
	        <label class="field"><span>Сипаттама</span><textarea name="description">${esc((course && course.description) || "")}</textarea></label>
	        <div class="grid three">
	          <label class="field"><span>Статус</span><select name="status">
	            ${["active", "inactive", "archived"].map((item) => `<option value="${item}" ${(course && course.status) === item ? "selected" : ""}>${esc(statusText[item] || item)}</option>`).join("")}
	          </select></label>
	          <label class="field"><span>Реті</span><input name="sort_order" type="number" min="1" value="${esc((course && course.sort_order) || nextOrder)}" /></label>
	          <label class="field"><span>Default access</span><select name="default_access_duration_type">
	            ${["lifetime", "30_days", "90_days", "custom"].map((item) => `<option value="${item}" ${duration === item ? "selected" : ""}>${esc(statusText[item] || item)}</option>`).join("")}
	          </select></label>
	        </div>
	        <label class="field"><span>Custom default date</span><input name="default_access_expires_at" type="date" value="${esc((course && course.default_access_expires_at || "").slice(0, 10))}" /></label>
	        <div class="grid two">
	          <label class="field"><span>Cover URL</span><input name="cover_image_url" placeholder="https://" value="${esc((course && course.cover_image_url) || "")}" /></label>
	          <label class="field"><span>Telegram канал ID</span><input name="telegram_chat_id" placeholder="2351826422 немесе -100..." value="${esc((course && course.telegram_chat_id) || "")}" /></label>
	        </div>
	        <label class="upload-drop premium-course-upload">
	          <input name="cover_upload" type="file" accept=".jpg,.jpeg,.png,.webp,image/*" />
	          <span class="upload-title">Cover жүктеу</span>
	          <small>JPG, PNG немесе WEBP</small>
	          <strong id="premiumCourseUploadName">Файл таңдалмады</strong>
	        </label>
	        <input type="hidden" name="cover_image_path" value="${esc((course && course.cover_image_path) || "")}" />
	        <input type="hidden" name="cover_image_source" value="${esc((course && course.cover_image_source) || "none")}" />
	        <div id="premiumCourseCoverPreview" class="admin-media-preview">${image ? `<img src="${esc(image)}" alt="${esc((course && course.title) || "Premium курс")}" />` : `<span class="muted small">Алдын ала көру</span>`}</div>
	        <div class="grid two">
	          <label class="field"><span>Invite түрі</span><select name="invite_link_type">
	            <option value="bot" ${inviteType === "bot" ? "selected" : ""}>Bot арқылы</option>
	            <option value="manual" ${inviteType === "manual" ? "selected" : ""}>Қолмен сілтеме</option>
	          </select></label>
	          <label class="field"><span>Қолмен сілтеме</span><input name="manual_invite_link" placeholder="${esc(DEFAULT_CHANNEL_LINK)}" value="${esc((course && course.manual_invite_link) || "")}" /></label>
	        </div>
	        <label class="field"><span>Telegram батырма атауы</span><input name="telegram_button_title" placeholder="Telegram каналға кіру" value="${esc((course && course.telegram_button_title) || "")}" /></label>
	        <label class="field"><span>Admin notes</span><textarea name="admin_notes">${esc((course && course.admin_notes) || "")}</textarea></label>
	        <div class="action-row end">
	          <button class="ghost-btn" data-close-modal type="button">Болдырмау</button>
	          <button class="gold-btn" type="submit"><span class="btn-label">Сақтау</span><span class="btn-spinner"></span></button>
	        </div>
	      </form>
	    `);
	    const form = shell.body.querySelector("form");
	    const upload = form.querySelector("input[name=cover_upload]");
	    const uploadName = shell.body.querySelector("#premiumCourseUploadName");
	    const preview = shell.body.querySelector("#premiumCourseCoverPreview");
	    shell.body.querySelector("[data-close-modal]").addEventListener("click", shell.close);
	    upload.addEventListener("change", async () => {
	      const file = upload.files && upload.files[0];
	      uploadName.textContent = file ? file.name : "Файл таңдалмады";
	      if (!file) return;
	      const fd = new FormData();
	      fd.append("image", file);
	      try {
	        const res = await api("/api/admin/premium-courses/upload-cover", { method: "POST", body: fd });
	        form.elements.cover_image_path.value = res.cover_image_path || "";
	        form.elements.cover_image_source.value = "uploaded";
	        form.elements.cover_image_url.value = "";
	        preview.innerHTML = res.cover_image_path ? `<img src="${esc(res.cover_image_path)}" alt="Premium course cover" />` : "";
	        toast("Cover жүктелді", "success");
	      } catch (error) {
	        toast(error.message || "Cover жүктеу мүмкін болмады", "error");
	      }
	    });
	    form.addEventListener("submit", async (event) => {
	      event.preventDefault();
	      const btn = form.querySelector("button[type=submit]");
	      const fd = new FormData(form);
	      const coverURL = compact(fd.get("cover_image_url"));
	      const coverPath = coverURL ? "" : compact(fd.get("cover_image_path"));
	      const defaultDate = compact(fd.get("default_access_expires_at"));
	      const payload = {
	        slug: compact(fd.get("slug")),
	        title: compact(fd.get("title")),
	        description: compact(fd.get("description")),
	        price_kzt: Number(fd.get("price_kzt") || 0),
	        status: compact(fd.get("status")) || "active",
	        sort_order: Number(fd.get("sort_order") || 0),
	        default_access_duration_type: compact(fd.get("default_access_duration_type")) || "lifetime",
	        default_access_expires_at: defaultDate ? new Date(defaultDate).toISOString() : null,
	        cover_image_url: coverURL,
	        cover_image_path: coverPath,
	        cover_image_source: coverPath ? "uploaded" : coverURL ? "url" : "none",
	        telegram_chat_id: compact(fd.get("telegram_chat_id")),
	        invite_link_type: compact(fd.get("invite_link_type")) || "manual",
	        manual_invite_link: compact(fd.get("manual_invite_link")),
	        telegram_button_title: compact(fd.get("telegram_button_title")),
	        admin_notes: compact(fd.get("admin_notes")),
	      };
	      if (!payload.slug || !/^[a-z0-9_-]+$/.test(payload.slug)) {
	        toast("Slug тек кіші әріп, сан, - немесе _ болуы керек", "error");
	        return;
	      }
	      if (!payload.title || payload.price_kzt <= 0) {
	        toast("Курс атауы және бағасы міндетті", "error");
	        return;
	      }
	      if (payload.cover_image_url && !isValidHTTPURL(payload.cover_image_url)) {
	        toast("Cover URL жарамсыз", "error");
	        return;
	      }
	      if (payload.invite_link_type === "manual" && payload.manual_invite_link && !isValidTelegramLink(payload.manual_invite_link)) {
	        toast("Telegram link https://t.me/... форматында болуы керек", "error");
	        return;
	      }
	      if (buttonIsLoading(btn)) return;
	      setButtonLoading(btn, true);
	      setModalBusy(shell, true);
	      try {
	        await api(isEdit ? `/api/admin/premium-courses/${course.id}` : "/api/admin/premium-courses", {
	          method: isEdit ? "PATCH" : "POST",
	          body: JSON.stringify(payload),
	        });
	        shell.close();
	        toast("Premium курс сақталды", "success");
	        renderAdminPremiumCourses();
	      } catch (error) {
	        toast(error.message || "Premium курсты сақтау мүмкін болмады", "error");
	      } finally {
	        setModalBusy(shell, false);
	        setButtonLoading(btn, false);
	      }
	    });
	  }

	  async function renderAdminFreeLessons() {
	    const data = await api("/api/admin/free-lessons");
	    const lessons = data.free_lessons || [];
	    els.adminContent.innerHTML = `
	      <div class="card">
	        <div class="admin-section-head">
	          <div><p class="eyebrow">Ашық контент</p><h2>Тегін сабақтар</h2></div>
	          <div class="admin-toolbar"><button class="gold-btn" id="addFreeLesson" type="button">+ Тегін сабақ қосу</button></div>
	        </div>
	        ${freeLessonTableHtml(lessons)}
	      </div>
	    `;
	    on("addFreeLesson", () => openFreeLessonModal(null, lessons));
	    delegate(els.adminContent, "[data-edit-free-lesson]", "click", (event, target) => {
	      const lesson = lessons.find((item) => item.id === target.dataset.editFreeLesson);
	      if (lesson) openFreeLessonModal(lesson, lessons);
	    });
	    delegate(els.adminContent, "[data-archive-free-lesson]", "click", async (event, target) => {
	      const lesson = lessons.find((item) => item.id === target.dataset.archiveFreeLesson);
	      const ok = await confirmAction({
	        title: "Тегін сабақты белсенді емес ету",
	        body: `<p class="muted">${esc(lesson ? lesson.title : "Бұл сабақ")} клиент жағында көрінбейді. Жалғастырасыз ба?</p>`,
	        confirmLabel: "Белсенді емес",
	        action: () => api(`/api/admin/free-lessons/${target.dataset.archiveFreeLesson}`, { method: "DELETE" }),
	        successMessage: "Тегін сабақ белсенді емес күйге ауысты",
	        errorMessage: "Тегін сабақты өзгерту мүмкін болмады",
	      });
	      if (!ok) return;
	      renderAdminFreeLessons();
	    });
	  }

	  function freeLessonTableHtml(lessons) {
	    if (!lessons.length) return emptyState("Тегін сабақтар табылмады");
	    return `<div class="table-wrap"><table>
	      <thead><tr><th>Сурет</th><th>Сабақ</th><th>YouTube</th><th>Реті</th><th>Статус</th><th>Жаңартылды</th><th>Әрекет</th></tr></thead>
	      <tbody>${lessons
	        .map((lesson) => {
	          const image = visibleFreeLessonImage(lesson);
	          return `<tr>
	            <td>${image ? `<img class="tariff-admin-thumb" src="${esc(image)}" alt="${esc(lesson.title)}" loading="lazy" />` : "—"}</td>
	            <td><strong>${esc(lesson.title)}</strong><div class="muted small">${esc(shortId(lesson.id))}</div><div class="muted small">${esc(shortText(lesson.short_description || lesson.description, 110))}</div></td>
	            <td>${lesson.youtube_embed_url ? `<a class="link" href="${esc(lesson.youtube_embed_url)}" target="_blank" rel="noopener">Алдын ала көру</a><div class="muted small">${esc(lesson.youtube_video_id || "")}</div>` : "—"}</td>
	            <td>${esc(lesson.sort_order || 0)}</td>
	            <td>${statusBadge(lesson.is_active ? "active" : "inactive")}</td>
	            <td>${formatDateTime(lesson.updated_at || lesson.created_at)}</td>
	            <td><div class="action-row">
	              <button class="ghost-btn" data-edit-free-lesson="${esc(lesson.id)}" type="button">Өңдеу</button>
	              <button class="danger-btn" data-archive-free-lesson="${esc(lesson.id)}" type="button">Жою</button>
	            </div></td>
	          </tr>`;
	        })
	        .join("")}</tbody></table></div>`;
	  }

	  function openFreeLessonModal(lesson, lessons) {
	    const isEdit = Boolean(lesson && lesson.id);
	    const image = visibleFreeLessonImage(lesson);
	    const embed = youtubeEmbedURL(lesson);
	    const nextOrder = Math.max(0, ...lessons.map((item) => Number(item.sort_order) || 0)) + 1;
	    const shell = openModalShell(isEdit ? "Тегін сабақты өңдеу" : "Тегін сабақ қосу", `
	      <form id="freeLessonModalForm" class="form">
	        <label class="field"><span>Атауы</span><input name="title" required value="${esc((lesson && lesson.title) || "")}" /></label>
	        <label class="field"><span>Қысқаша сипаттама</span><input name="short_description" value="${esc((lesson && lesson.short_description) || "")}" /></label>
	        <label class="field"><span>Толық сипаттама</span><textarea name="description" required>${esc((lesson && lesson.description) || "")}</textarea></label>
	        <div class="grid two">
	          <label class="field"><span>Сурет URL</span><input name="image_url" placeholder="https://" value="${esc((lesson && lesson.image_url) || "")}" /></label>
	          <label class="field"><span>Реті</span><input name="sort_order" type="number" min="1" value="${esc((lesson && lesson.sort_order) || nextOrder)}" /></label>
	        </div>
	        <label class="upload-drop free-lesson-upload">
	          <input name="image_upload" type="file" accept=".jpg,.jpeg,.png,.webp,image/*" />
	          <span class="upload-title">Сурет жүктеу</span>
	          <small>JPG, PNG немесе WEBP, 5 MB дейін</small>
	          <strong id="freeLessonUploadName">Файл таңдалмады</strong>
	        </label>
	        <input type="hidden" name="image_file_path" value="${esc((lesson && lesson.image_file_path) || "")}" />
	        <input type="hidden" name="image_source" value="${esc((lesson && lesson.image_source) || "none")}" />
	        <div id="freeLessonImagePreview" class="admin-media-preview">${image ? `<img src="${esc(image)}" alt="${esc((lesson && lesson.title) || "Тегін сабақ")}" />` : `<span class="muted small">Алдын ала көру</span>`}</div>
	        <label class="field"><span>YouTube сілтемесі</span><input name="youtube_url" required placeholder="https://youtu.be/VIDEO_ID" value="${esc((lesson && lesson.youtube_url) || "")}" /><small>YouTube Share, watch, shorts немесе embed сілтемесін қойыңыз.</small></label>
	        <div id="freeLessonYoutubePreview" class="admin-youtube-preview">${embed ? youtubeIframe(embed, (lesson && lesson.title) || "YouTube") : `<span class="muted small">YouTube алдын ала көру</span>`}</div>
	        <label class="switch-field"><input name="is_active" type="checkbox" ${!lesson || lesson.is_active ? "checked" : ""} /><span>Белсенді</span></label>
	        <div class="action-row end">
	          <button class="ghost-btn" data-close-modal type="button">Болдырмау</button>
	          <button class="gold-btn" type="submit"><span class="btn-label">Сақтау</span><span class="btn-spinner"></span></button>
	        </div>
	      </form>
	    `);
	    const form = shell.body.querySelector("form");
	    const imageInput = form.elements.image_url;
	    const youtubeInput = form.elements.youtube_url;
	    const upload = form.querySelector("input[name=image_upload]");
	    const uploadName = shell.body.querySelector("#freeLessonUploadName");
	    const imagePreview = shell.body.querySelector("#freeLessonImagePreview");
	    const youtubePreview = shell.body.querySelector("#freeLessonYoutubePreview");
	    const setImagePreview = (src, message) => {
	      imagePreview.innerHTML = src ? `<img src="${esc(src)}" alt="Сурет алдын ала көру" />` : `<span class="muted small">${esc(message || "Алдын ала көру")}</span>`;
	    };
	    const setYoutubePreview = () => {
	      const id = parseYouTubeVideoID(youtubeInput.value);
	      const embed = youtubeEmbedURLFromID(id);
	      if (id) {
	        youtubePreview.innerHTML = youtubeIframe(embed, "YouTube алдын ала көру");
	      } else {
	        youtubePreview.innerHTML = `<span class="muted small">${compact(youtubeInput.value) ? "YouTube сілтемесі жарамсыз" : "YouTube алдын ала көру"}</span>`;
	      }
	    };
	    shell.body.querySelector("[data-close-modal]").addEventListener("click", shell.close);
	    imageInput.addEventListener("input", () => {
	      const raw = compact(imageInput.value);
	      if (!raw) {
	        setImagePreview(compact(form.elements.image_file_path.value), "Алдын ала көру");
	        return;
	      }
	      setImagePreview(isValidHTTPURL(raw) ? raw : "", isValidHTTPURL(raw) ? "" : "Сурет URL жарамсыз");
	    });
	    youtubeInput.addEventListener("input", setYoutubePreview);
	    upload.addEventListener("change", async () => {
	      const file = upload.files && upload.files[0];
	      uploadName.textContent = file ? file.name : "Файл таңдалмады";
	      if (!file) return;
	      const fd = new FormData();
	      fd.append("image", file);
	      try {
	        const res = await api("/api/admin/free-lessons/upload-image", { method: "POST", body: fd });
	        form.elements.image_file_path.value = res.image_file_path || "";
	        form.elements.image_source.value = "uploaded";
	        form.elements.image_url.value = "";
	        setImagePreview(res.image_file_path || "", "Алдын ала көру");
	        toast("Сурет жүктелді", "success");
	      } catch (error) {
	        toast(error.message || "Сурет жүктеу мүмкін болмады", "error");
	      }
	    });
	    form.addEventListener("submit", async (event) => {
	      event.preventDefault();
	      const btn = form.querySelector("button[type=submit]");
	      const fd = new FormData(form);
	      const imageURL = compact(fd.get("image_url"));
	      const youtubeURL = compact(fd.get("youtube_url"));
	      const imageFilePath = imageURL ? "" : compact(fd.get("image_file_path"));
	      const payload = {
	        title: compact(fd.get("title")),
	        short_description: compact(fd.get("short_description")),
	        description: compact(fd.get("description")),
	        image_url: imageURL,
	        image_file_path: imageFilePath,
	        image_source: imageFilePath ? "uploaded" : imageURL ? "url" : "none",
	        youtube_url: youtubeURL,
	        sort_order: Number(fd.get("sort_order") || 0),
	        is_active: fd.get("is_active") === "on",
	      };
	      if (!payload.title || !payload.description) {
	        toast("Атауы және толық сипаттама міндетті", "error");
	        return;
	      }
	      if (!payload.image_url && !payload.image_file_path) {
	        toast("Суретті URL арқылы немесе файл жүктеп қосыңыз", "error");
	        return;
	      }
	      if (payload.image_url && !isValidHTTPURL(payload.image_url)) {
	        toast("Сурет URL жарамсыз", "error");
	        return;
	      }
	      if (!parseYouTubeVideoID(payload.youtube_url)) {
	        toast("YouTube сілтемесі жарамсыз", "error");
	        return;
	      }
	      if (buttonIsLoading(btn)) return;
	      setButtonLoading(btn, true);
	      setModalBusy(shell, true);
	      try {
	        await api(isEdit ? `/api/admin/free-lessons/${lesson.id}` : "/api/admin/free-lessons", {
	          method: isEdit ? "PATCH" : "POST",
	          body: JSON.stringify(payload),
	        });
	        shell.close();
	        toast("Тегін сабақ сақталды", "success");
	        renderAdminFreeLessons();
	      } catch (error) {
	        toast(error.message || "Тегін сабақты сақтау мүмкін болмады", "error");
	      } finally {
	        setModalBusy(shell, false);
	        setButtonLoading(btn, false);
	      }
	    });
	  }

		  async function renderAdminItems(url, title) {
	    const data = await api(url);
    const rows = data.items || [];
    const columns = rows[0] ? Object.keys(rows[0]).slice(0, 7) : ["id", "status"];
    els.adminContent.innerHTML = `
      <div class="card">
        <div class="admin-section-head">
          <div><p class="eyebrow">${esc(title || "Жазбалар")}</p><h2>${esc(title || "Жазбалар")}</h2></div>
        </div>
        ${tableHtml(columns, rows)}
      </div>
    `;
  }

	  async function adminLevels() {
	    const data = await api("/api/admin/levels");
	    return sortAdminLevels(data.levels || []);
	  }

	  async function adminLessons(params) {
	    const data = await api(`/api/admin/lessons?${params || ""}`);
	    return data.lessons || [];
	  }

  function levelSortValue(level) {
    return Number(level && (level.sort_order || level.number)) || 0;
  }

  function sortAdminLevels(levels) {
    return [...(levels || [])].sort((a, b) => {
      const order = levelSortValue(a) - levelSortValue(b);
      if (order) return order;
      return (Number(a.number) || 0) - (Number(b.number) || 0);
    });
  }

	  function levelDisplayName(level) {
	    if (!level) return "Деңгей";
	    const title = clean(level.title_kk || level.title || "");
	    return `Деңгей ${level.number || level.sort_order || "—"}${title ? ` — ${title}` : ""}`;
	  }

  function isSeededLevel(level) {
    const number = Number(level && level.number);
    return number >= 1 && number <= 12;
  }

  function levelOptions(levels, selected) {
    return sortAdminLevels(levels)
      .map(
        (level) =>
          `<option value="${esc(level.id)}" ${String(level.id) === String(selected) ? "selected" : ""}>${esc(levelDisplayName(level))}${level.is_active ? "" : " · Белсенді емес"}</option>`,
      )
      .join("");
  }

	  function levelNumberOptions(levels, selected) {
    return sortAdminLevels(levels)
      .map(
        (level) =>
          `<option value="${esc(level.number)}" ${String(level.number) === String(selected) ? "selected" : ""}>${esc(levelDisplayName(level))}${level.is_active ? "" : " · Белсенді емес"}</option>`,
      )
	      .join("");
	  }

	  function lessonDisplayName(lesson) {
	    if (!lesson) return "Сабақ";
	    const level = lesson.level_number ? `Деңгей ${lesson.level_number}` : "Деңгей";
	    const title = clean(lesson.title_kk || lesson.lesson_title_kk || lesson.title || "");
	    return `${level}${title ? ` — ${title}` : ""}`;
	  }

	  function lessonOptions(lessons, selected) {
	    return [...(lessons || [])]
	      .sort((a, b) => (Number(a.level_number) || 0) - (Number(b.level_number) || 0) || (Number(a.sort_order) || 0) - (Number(b.sort_order) || 0))
	      .map((lesson) => `<option value="${esc(lesson.id)}" ${String(lesson.id) === String(selected) ? "selected" : ""}>${esc(lessonDisplayName(lesson))}${lesson.is_active ? "" : " · Белсенді емес"}</option>`)
	      .join("");
	  }

	  function currentAdminFilters() {
	    const q = document.getElementById("adminSearch")?.value || "";
	    const level = document.getElementById("adminLevelFilter")?.value || "";
	    const lesson = document.getElementById("adminLessonFilter")?.value || "";
	    const status = document.getElementById("adminStatusFilter")?.value || "";
	    return { q, level, lesson, status };
	  }

  async function renderAdminLevels() {
    const levels = await adminLevels();
    els.adminContent.innerHTML = `
      <div class="card">
        <div class="admin-section-head">
          <div><p class="eyebrow">Деңгейлер</p><h2>Деңгейлер</h2></div>
          <div class="admin-toolbar">
            <button class="gold-btn" id="addLevel" type="button">+ Деңгей қосу</button>
          </div>
        </div>
        ${levelsTableHtml(levels)}
      </div>
    `;
    on("addLevel", () => openLevelModal(null, levels));
    delegate(els.adminContent, "[data-edit-level]", "click", (event, target) => {
      event.stopPropagation();
      const level = levels.find((item) => item.id === target.dataset.editLevel);
      if (level) openLevelModal(level, levels);
    });
    delegate(els.adminContent, "[data-delete-level]", "click", async (event, target) => {
      event.stopPropagation();
      const level = levels.find((item) => item.id === target.dataset.deleteLevel);
      const ok = await confirmAction({
        title: "Деңгейді жою",
        body: `<p class="muted">${esc(level ? levelDisplayName(level) : "Бұл деңгей")} өшіруге сенімдісіз бе?</p>`,
        cancelLabel: "Бас тарту",
        confirmLabel: "Жою",
        action: () => api(`/api/admin/levels/${target.dataset.deleteLevel}`, { method: "DELETE" }),
        successMessage: "Деңгей өшірілді",
        formatError: levelDeleteErrorMessage,
      });
      if (!ok) return;
      renderAdminLevels();
    });
  }

  function levelsTableHtml(levels) {
    if (!levels.length) return emptyState("Деңгейлер табылмады");
    return `<div class="table-wrap"><table>
      <thead><tr>
        <th>Реті</th><th>Қазақша атауы</th><th>Telegram</th><th>Сипаттама</th><th>Статус</th><th>Әрекет</th>
      </tr></thead>
      <tbody>${levels
        .map(
          (level) => {
            const seeded = isSeededLevel(level);
            return `<tr>
	            <td><strong>Деңгей ${esc(level.number || "—")}</strong><div class="muted small">ID ${esc(shortId(level.id))}</div></td>
            <td><strong>${esc(level.title_kk || "—")}</strong></td>
            <td><span class="muted small">${esc(shortId(level.telegram_chat_id || ""))}</span></td>
            <td>${esc(level.description_kk || "—")}</td>
            <td>${statusBadge(level.is_active ? "active" : "inactive")}</td>
            <td>
              <div class="action-row">
                <button class="ghost-btn" data-edit-level="${esc(level.id)}" type="button">Өзгерту</button>
                <button class="danger-btn" data-delete-level="${esc(level.id)}" type="button" ${seeded ? `disabled title="Негізгі 12 деңгейді өшіруге болмайды"` : ""}>Жою</button>
              </div>
            </td>
          </tr>`;
          },
        )
        .join("")}</tbody>
    </table></div>`;
  }

  function nextLevelNumber(levels) {
    const max = levels.reduce((acc, level) => {
      const value = Math.max(Number(level.number) || 0, Number(level.sort_order) || 0);
      return Math.max(acc, value);
    }, 0);
    return max + 1;
  }

  function openLevelModal(level, levels) {
    const isEdit = Boolean(level && level.id);
    const seeded = isSeededLevel(level);
    const number = isEdit ? level.number : nextLevelNumber(levels);
    const active = !isEdit || level.is_active;
    els.adminContent.innerHTML = `
      <section class="admin-form-page">
        <div class="admin-section-head">
          <div><p class="eyebrow">Деңгей</p><h2>${isEdit ? "Деңгейді жаңарту" : "Деңгей қосу"}</h2></div>
          <button class="ghost-btn" id="backToLevels" type="button">Болдырмау</button>
        </div>
        <form id="levelModalForm" class="form admin-form-card">
          <div class="grid two">
            <label class="field"><span>Деңгей нөмірі / реті</span><input name="number" type="number" min="1" step="1" required ${seeded ? "readonly" : ""} value="${esc(number)}" /></label>
            <label class="switch-field"><input name="is_active" type="checkbox" ${active ? "checked" : ""} ${seeded ? "disabled" : ""} /><span>Белсенді</span></label>
          </div>
          ${seeded ? `<p class="muted small">Негізгі 12 деңгей жүйеде әрқашан белсенді сақталады.</p>` : ""}
          <label class="field"><span>Қазақша атауы</span><input name="title_kk" required value="${esc((level && level.title_kk) || "")}" /></label>
          <label class="field"><span>Сипаттама</span><textarea name="description_kk">${esc((level && level.description_kk) || "")}</textarea></label>
          <label class="field"><span>Telegram канал ID</span><input name="telegram_chat_id" inputmode="text" placeholder="2351826422 немесе -1002351826422" value="${esc((level && level.telegram_chat_id) || "")}" /><small>Мысалы: 2351826422 немесе -1002351826422. Бот осы каналда админ болуы керек.</small></label>
          <div class="action-row end">
            <button class="ghost-btn" id="cancelLevelForm" type="button">Болдырмау</button>
            <button class="gold-btn" type="submit"><span class="btn-label">${isEdit ? "Жаңарту" : "Сақтау"}</span><span class="btn-spinner"></span></button>
          </div>
        </form>
      </section>
    `;
    const form = document.getElementById("levelModalForm");
    on("backToLevels", renderAdminLevels);
    on("cancelLevelForm", renderAdminLevels);
    form.addEventListener("submit", async (event) => {
      event.preventDefault();
      const btn = form.querySelector("button[type=submit]");
      const payload = collectLevelForm(form, level);
      const validation = validateLevelForm(payload, levels, level);
      if (validation) {
        toast(validation, "error");
        return;
      }
      if (buttonIsLoading(btn)) return;
      setButtonLoading(btn, true);
      try {
        await api(isEdit ? `/api/admin/levels/${level.id}` : "/api/admin/levels", {
          method: isEdit ? "PATCH" : "POST",
          body: JSON.stringify(payload),
        });
        toast(isEdit ? "Деңгей жаңартылды" : "Деңгей қосылды", "success");
        renderAdminLevels();
      } catch (error) {
        toast(levelSaveErrorMessage(error), "error");
      } finally {
        setButtonLoading(btn, false);
      }
    });
  }

  function collectLevelForm(form, existing) {
    const fd = new FormData(form);
    const seeded = isSeededLevel(existing);
    const number = seeded ? Number(existing.number) : Number(fd.get("number") || 0);
    const titleKK = String(fd.get("title_kk") || "").trim();
    const descriptionKK = String(fd.get("description_kk") || "").trim();
    return {
      number,
      sort_order: number,
      title_kk: titleKK,
      title_ru: clean(existing && existing.title_ru) || titleKK,
      description_kk: descriptionKK,
      description_ru: clean(existing && existing.description_ru) || descriptionKK,
      telegram_chat_id: String(fd.get("telegram_chat_id") || "").trim(),
      is_active: seeded ? true : fd.get("is_active") === "on",
    };
  }

  function validateLevelForm(level, levels, existing) {
    if (!Number.isInteger(level.number) || level.number < 1) return "Деңгей нөмірі 1 немесе одан жоғары болуы керек";
    if (!level.title_kk) return "Қазақша атауы міндетті";
    if (level.telegram_chat_id && !/^(?:[1-9]\d*|-100[1-9]\d*)$/.test(level.telegram_chat_id)) return "Telegram канал ID: 2351826422 немесе -1002351826422 форматында жазыңыз";
    const duplicate = levels.find((item) => item.id !== (existing && existing.id) && Number(item.number) === level.number);
    if (duplicate) return "Бұл деңгей нөмірі бұрыннан бар";
    return "";
  }

  function levelSaveErrorMessage(error) {
    const raw = String((error && error.message) || "");
    if (/unique|constraint|duplicate/i.test(raw)) return "Бұл деңгей нөмірі бұрыннан бар";
    if (/invalid state/i.test(raw)) return "Деңгей мәліметтерін тексеріңіз";
    return raw || "Деңгейді сақтау мүмкін болмады";
  }

  function levelDeleteErrorMessage(error) {
    const raw = String((error && error.message) || "");
    if (/lesson|test|foreign|constraint/i.test(raw)) {
      return "Бұл деңгейге сабақтар немесе тесттер байланған. Алдымен контентті өзгертіңіз немесе деңгейді белсенді емес күйге қойыңыз.";
    }
    return raw || "Деңгейді өшіру мүмкін болмады";
  }

  async function renderAdminLessons() {
    const params = new URLSearchParams();
    const filters = currentAdminFilters();
    if (filters.q) params.set("q", filters.q);
    if (filters.level) params.set("level", filters.level);
    if (filters.status) params.set("status", filters.status);

    const [levels, data] = await Promise.all([adminLevels(), api(`/api/admin/lessons?${params.toString()}`)]);
    const lessons = data.lessons || [];
    const rows = lessons.length
      ? `<div class="table-wrap"><table>
          <thead><tr>
            <th>Деңгей</th><th>Сабақ атауы</th><th>Сілтеме</th><th>Реті</th><th>Статус</th><th>Әрекет</th>
          </tr></thead>
          <tbody>${lessons
            .map(
              (lesson) => `<tr>
	                <td>Деңгей ${esc(lesson.level_number)}</td>
                <td><strong>${esc(lesson.title_kk)}</strong><div class="muted small">${esc(shortId(lesson.id))}</div></td>
                <td>${lesson.video_url ? `<a class="link" href="${esc(lesson.video_url)}" target="_blank" rel="noopener">Ашу</a>` : "—"}</td>
                <td>${esc(lesson.sort_order || 0)}</td>
                <td>${statusBadge(lesson.is_active ? "active" : "inactive")}</td>
                <td>
                  <div class="action-row">
                    <button class="ghost-btn" data-edit-lesson="${esc(lesson.id)}" type="button">Өзгерту</button>
	                    <button class="ghost-btn" data-test-lesson="${esc(lesson.id)}" type="button">Осы сабаққа тест қосу</button>
                    <button class="danger-btn" data-delete-lesson="${esc(lesson.id)}" type="button">Өшіру</button>
                  </div>
                </td>
              </tr>`,
            )
            .join("")}</tbody>
        </table></div>`
      : emptyState("Сабақтар әлі қосылған жоқ");

    els.adminContent.innerHTML = `
      <div class="card">
        <div class="admin-section-head">
          <div><p class="eyebrow">Контент</p><h2>Сабақтар</h2></div>
          <div class="admin-toolbar">
            <button class="gold-btn" id="addLesson" type="button">+ Сабақ қосу</button>
          </div>
        </div>
        <div class="admin-toolbar stacked">
          <input id="adminSearch" placeholder="Сабақ іздеу" value="${esc(filters.q)}" />
          <select id="adminLevelFilter"><option value="">Барлық деңгей</option>${levelNumberOptions(levels, filters.level)}</select>
          <select id="adminStatusFilter"><option value="">Барлық статус</option><option value="active" ${filters.status === "active" ? "selected" : ""}>Белсенді</option><option value="inactive" ${filters.status === "inactive" ? "selected" : ""}>Жабық</option></select>
        </div>
        ${rows}
      </div>
    `;

    on("addLesson", () => openLessonModal(null, levels));
    on("adminSearch", debounce(renderAdminLessons, 280), "input");
    on("adminLevelFilter", renderAdminLessons, "change");
    on("adminStatusFilter", renderAdminLessons, "change");
    delegate(els.adminContent, "[data-edit-lesson]", "click", (event, target) => {
      event.stopPropagation();
      const lesson = lessons.find((item) => item.id === target.dataset.editLesson);
      if (lesson) openLessonModal(lesson, levels);
    });
    delegate(els.adminContent, "[data-delete-lesson]", "click", async (event, target) => {
      event.stopPropagation();
      const ok = await confirmAction({
        title: "Сабақты өшіру",
        body: `<p class="muted">Бұл сабақты өшіруге сенімдісіз бе?</p>`,
        confirmLabel: "Өшіру",
        action: () => api(`/api/admin/lessons/${target.dataset.deleteLesson}`, { method: "DELETE" }),
        successMessage: "Сабақ өшірілді",
        errorMessage: "Өшіру мүмкін болмады",
      });
      if (!ok) return;
      renderAdminLessons();
    });
	    delegate(els.adminContent, "[data-test-lesson]", "click", (event, target) => {
	      event.stopPropagation();
	      state.adminScreen = "tests";
	      renderAdminNav();
	      renderAdmin().then(async () => {
	        const [nextLevels, nextLessons] = await Promise.all([adminLevels(), adminLessons()]);
	        const lesson = nextLessons.find((item) => item.id === target.dataset.testLesson);
	        if (lesson) openTestModal({ lesson_id: lesson.id, level_number: lesson.level_number, lesson_title_kk: lesson.title_kk }, nextLevels, nextLessons);
	      });
	    });
  }

  function openLessonModal(lesson, levels) {
    const isEdit = Boolean(lesson && lesson.id);
    const selectedLevel = (lesson && lesson.level_id) || (levels[0] && levels[0].id) || "";
    els.adminContent.innerHTML = `
      <section class="admin-form-page">
        <div class="admin-section-head">
          <div><p class="eyebrow">Сабақ</p><h2>${isEdit ? "Сабақты өзгерту" : "Сабақ қосу"}</h2></div>
          <button class="ghost-btn" id="backToLessons" type="button">Болдырмау</button>
        </div>
        <form id="lessonModalForm" class="form admin-form-card">
          <label class="field"><span>Деңгей</span><select name="level_id" required>${levelOptions(levels, selectedLevel)}</select></label>
          <div class="grid two">
            <label class="field"><span>Қазақша атауы</span><input name="title_kk" required value="${esc((lesson && lesson.title_kk) || "")}" /></label>
            <label class="field"><span>Орысша атауы</span><input name="title_ru" value="${esc((lesson && lesson.title_ru) || "")}" /></label>
          </div>
          <label class="field"><span>Сабақ сілтемесі</span><input name="video_url" placeholder="Қосымша видео немесе пост URL" value="${esc((lesson && lesson.video_url) || "")}" /><small>Қосымша өріс. Қорғалған Telegram материалдар деңгейдегі канал арқылы беріледі.</small></label>
          <div class="grid two">
            <label class="field"><span>Сипаттама KK</span><textarea name="description_kk">${esc((lesson && lesson.description_kk) || "")}</textarea></label>
            <label class="field"><span>Сипаттама RU</span><textarea name="description_ru">${esc((lesson && lesson.description_ru) || "")}</textarea></label>
          </div>
          <div class="grid two">
            <label class="field"><span>Реті</span><input name="sort_order" type="number" min="1" value="${esc((lesson && lesson.sort_order) || 1)}" /></label>
            <label class="switch-field"><input name="is_active" type="checkbox" ${!lesson || lesson.is_active ? "checked" : ""} /><span>Белсенді</span></label>
          </div>
          <div class="action-row end">
            <button class="ghost-btn" id="cancelLessonForm" type="button">Болдырмау</button>
            <button class="gold-btn" type="submit"><span class="btn-label">Сақтау</span><span class="btn-spinner"></span></button>
          </div>
        </form>
      </section>
    `;
    on("backToLessons", renderAdminLessons);
    on("cancelLessonForm", renderAdminLessons);
    document.getElementById("lessonModalForm").addEventListener("submit", async (event) => {
      event.preventDefault();
      const btn = event.currentTarget.querySelector("button[type=submit]");
      const form = new FormData(event.currentTarget);
      const body = Object.fromEntries(form.entries());
      body.sort_order = Number(body.sort_order || 1);
      body.is_active = form.get("is_active") === "on";
      body.video_url = String(body.video_url || "").trim();
      if (!body.level_id || !body.title_kk.trim()) {
        toast("Деңгей және сабақ атауы міндетті", "error");
        return;
      }
      if (buttonIsLoading(btn)) return;
      setButtonLoading(btn, true);
      try {
        const url = isEdit ? `/api/admin/lessons/${lesson.id}` : "/api/admin/lessons";
        await api(url, { method: isEdit ? "PATCH" : "POST", body: JSON.stringify(body) });
        toast("Сабақ сақталды", "success");
        renderAdminLessons();
      } catch (error) {
        toast(error.message || "Сақтау мүмкін болмады", "error");
      } finally {
        setButtonLoading(btn, false);
      }
    });
  }

	  async function renderAdminTests() {
	    const filters = currentAdminFilters();
	    const params = new URLSearchParams();
	    if (filters.q) params.set("q", filters.q);
	    if (filters.level) params.set("level", filters.level);
	    if (filters.lesson) params.set("lesson", filters.lesson);
	    if (filters.status) params.set("status", filters.status);
	    const [levels, lessons, data] = await Promise.all([adminLevels(), adminLessons(), api(`/api/admin/tests?${params.toString()}`)]);
	    const tests = data.tests || [];
	    const lessonFilterOptions = lessons.filter((lesson) => !filters.level || String(lesson.level_number) === String(filters.level));
	    const rows = tests.length
	      ? `<div class="table-wrap"><table>
	        <thead><tr><th>Сабақ</th><th>Тест атауы</th><th>Сұрақ саны</th><th>Өту пайызы</th><th>Статус</th><th>Әрекет</th></tr></thead>
	        <tbody>${tests
	          .map(
	            (test) => `<tr>
	              <td>${test.lesson_id ? esc(lessonDisplayName(test)) : `Деңгей ${esc(test.level_number)} · ескі жазба`}</td>
	              <td><strong>${esc(test.title)}</strong><div class="muted small">${esc(shortId(test.id))}</div></td>
	              <td>${esc((test.questions || []).length)}</td>
              <td>${esc(test.pass_percent)}%</td>
              <td>${statusBadge(test.is_active ? "active" : "inactive")}</td>
              <td><div class="action-row">
                <button class="ghost-btn" data-edit-test="${esc(test.id)}" type="button">Өзгерту</button>
                <button class="danger-btn" data-delete-test="${esc(test.id)}" type="button">Өшіру</button>
              </div></td>
            </tr>`,
          )
          .join("")}</tbody></table></div>`
      : emptyState("Тесттер әлі қосылған жоқ");
    els.adminContent.innerHTML = `
      <div class="card">
        <div class="admin-section-head">
          <div><p class="eyebrow">Білім тексеру</p><h2>Тесттер</h2></div>
          <div class="admin-toolbar"><button class="gold-btn" id="addTest" type="button">+ Тест қосу</button></div>
        </div>
	        <div class="admin-toolbar stacked">
	          <input id="adminSearch" placeholder="Тест іздеу" value="${esc(filters.q)}" />
	          <select id="adminLevelFilter"><option value="">Барлық деңгей</option>${levelNumberOptions(levels, filters.level)}</select>
	          <select id="adminLessonFilter"><option value="">Барлық сабақ</option>${lessonOptions(lessonFilterOptions, filters.lesson)}</select>
	          <select id="adminStatusFilter"><option value="">Барлық статус</option><option value="active" ${filters.status === "active" ? "selected" : ""}>Белсенді</option><option value="inactive" ${filters.status === "inactive" ? "selected" : ""}>Жабық</option></select>
	        </div>
        ${rows}
      </div>
    `;
	    on("addTest", () => openTestModal(null, levels, lessons));
	    on("adminSearch", debounce(renderAdminTests, 280), "input");
	    on("adminLevelFilter", renderAdminTests, "change");
	    on("adminLessonFilter", renderAdminTests, "change");
	    on("adminStatusFilter", renderAdminTests, "change");
	    delegate(els.adminContent, "[data-edit-test]", "click", (event, target) => {
	      const test = tests.find((item) => item.id === target.dataset.editTest);
	      if (test) openTestModal(test, levels, lessons);
	    });
    delegate(els.adminContent, "[data-delete-test]", "click", async (event, target) => {
      const ok = await confirmAction({
        title: "Тестті өшіру",
        body: `<p class="muted">Бұл тестті өшіруге сенімдісіз бе?</p>`,
        confirmLabel: "Өшіру",
        action: () => api(`/api/admin/tests/${target.dataset.deleteTest}`, { method: "DELETE" }),
        successMessage: "Тест өшірілді",
        errorMessage: "Өшіру мүмкін болмады",
      });
      if (!ok) return;
      renderAdminTests();
    });
  }

	  function defaultTest(lessons) {
	    return {
	      lesson_id: lessons[0] ? lessons[0].id : "",
	      title: "Сабақ тесті",
	      pass_percent: 70,
	      is_active: true,
      questions: [
        {
          question_text_kk: "",
          question_text_ru: "",
          options: [
            { option_text_kk: "", option_text_ru: "", is_correct: true },
            { option_text_kk: "", option_text_ru: "", is_correct: false },
          ],
        },
      ],
	    };
	  }

	  function openTestModal(test, levels, lessons) {
	    const model = test ? JSON.parse(JSON.stringify(test)) : defaultTest(lessons);
	    if (!model.questions || !model.questions.length) {
	      model.questions = defaultTest(lessons).questions;
	      if (!model.title) model.title = "Сабақ тесті";
	      if (!model.pass_percent) model.pass_percent = 70;
	      if (model.is_active === undefined) model.is_active = true;
	    }
	    if (!model.lesson_id && model.level_number) {
	      const byLevel = lessons.find((lesson) => Number(lesson.level_number) === Number(model.level_number));
	      if (byLevel) model.lesson_id = byLevel.id;
	    }
	    if (model.lesson_id && !lessons.some((lesson) => lesson.id === model.lesson_id)) {
	      model.lesson_id = lessons[0] ? lessons[0].id : "";
	    }
	    const isEdit = Boolean(model.id);
	    const shell = openModalShell(isEdit ? "Тестті өзгерту" : "Тест қосу", `
	      <form id="testBuilderForm" class="form">
	        <div class="grid three">
	          <label class="field"><span>Сабақ</span><select name="lesson_id" required>${lessonOptions(lessons, model.lesson_id)}</select></label>
	          <label class="field"><span>Тест атауы</span><input name="title" required value="${esc(model.title || "")}" /></label>
	          <label class="field"><span>Өту пайызы</span><input name="pass_percent" type="number" min="1" max="100" required value="${esc(model.pass_percent || 70)}" /></label>
        </div>
        <label class="switch-field"><input name="is_active" type="checkbox" ${model.is_active ? "checked" : ""} /><span>Белсенді</span></label>
        <div class="builder-head">
          <h3>Сұрақтар</h3>
          <button class="ghost-btn" id="addQuestion" type="button">Сұрақ қосу</button>
        </div>
        <div id="questionBuilderList" class="builder-list">${(model.questions || []).map(questionBuilderHtml).join("")}</div>
        <div class="action-row end">
          <button class="ghost-btn" data-close-modal type="button">Болдырмау</button>
          <button class="gold-btn" type="submit"><span class="btn-label">Сақтау</span><span class="btn-spinner"></span></button>
        </div>
      </form>
    `);
    const form = shell.body.querySelector("form");
    const list = shell.body.querySelector("#questionBuilderList");
    shell.body.querySelector("[data-close-modal]").addEventListener("click", shell.close);
    shell.body.querySelector("#addQuestion").addEventListener("click", () => {
      list.insertAdjacentHTML("beforeend", questionBuilderHtml({ question_text_kk: "", question_text_ru: "", options: [{ option_text_kk: "", is_correct: true }, { option_text_kk: "", is_correct: false }] }));
      renumberBuilder(list);
    });
    shell.body.addEventListener("click", (event) => {
      const btn = event.target.closest("[data-builder-action]");
      if (!btn) return;
      const action = btn.dataset.builderAction;
      const question = btn.closest(".builder-question");
      const option = btn.closest(".builder-option");
      if (action === "remove-question" && list.children.length > 1) question.remove();
      if (action === "add-option") question.querySelector(".builder-options").insertAdjacentHTML("beforeend", optionBuilderHtml({ option_text_kk: "", is_correct: false }));
      if (action === "remove-option" && question.querySelectorAll(".builder-option").length > 2) option.remove();
      if (action === "up" && question.previousElementSibling) list.insertBefore(question, question.previousElementSibling);
      if (action === "down" && question.nextElementSibling) list.insertBefore(question.nextElementSibling, question);
      if (action === "option-up" && option.previousElementSibling) option.parentElement.insertBefore(option, option.previousElementSibling);
      if (action === "option-down" && option.nextElementSibling) option.parentElement.insertBefore(option.nextElementSibling, option);
      renumberBuilder(list);
    });
    form.addEventListener("submit", async (event) => {
      event.preventDefault();
      const btn = form.querySelector("button[type=submit]");
      const payload = collectTestBuilder(form);
      const validation = validateTestBuilder(payload);
      if (validation) {
        toast(validation, "error");
        return;
      }
      if (buttonIsLoading(btn)) return;
      setButtonLoading(btn, true);
      setModalBusy(shell, true);
      try {
        await api(isEdit ? `/api/admin/tests/${model.id}` : "/api/admin/tests", {
          method: isEdit ? "PATCH" : "POST",
          body: JSON.stringify(payload),
        });
        shell.close();
        toast("Тест сақталды", "success");
        renderAdminTests();
      } catch (error) {
        toast(error.message || "Сақтау мүмкін болмады", "error");
      } finally {
        setModalBusy(shell, false);
        setButtonLoading(btn, false);
      }
    });
    renumberBuilder(list);
  }

  function questionBuilderHtml(question) {
    return `<section class="builder-question">
      <div class="builder-question-head">
        <strong data-question-number>Сұрақ</strong>
        <div class="action-row">
          <button class="ghost-btn icon-mini" data-builder-action="up" type="button">↑</button>
          <button class="ghost-btn icon-mini" data-builder-action="down" type="button">↓</button>
          <button class="danger-btn icon-mini" data-builder-action="remove-question" type="button">Өшіру</button>
        </div>
      </div>
      <label class="field"><span>Сұрақ мәтіні KK</span><textarea data-question-kk required>${esc(question.question_text_kk || "")}</textarea></label>
      <label class="field"><span>Сұрақ мәтіні RU</span><textarea data-question-ru>${esc(question.question_text_ru || "")}</textarea></label>
      <div class="builder-options">${(question.options || []).map(optionBuilderHtml).join("")}</div>
      <button class="ghost-btn" data-builder-action="add-option" type="button">Жауап қосу</button>
    </section>`;
  }

  function optionBuilderHtml(option) {
    return `<div class="builder-option">
      <input class="correct-radio" data-correct type="radio" ${option.is_correct ? "checked" : ""} />
      <label class="field"><span>Жауап KK</span><input data-option-kk required value="${esc(option.option_text_kk || "")}" /></label>
      <label class="field"><span>Жауап RU</span><input data-option-ru value="${esc(option.option_text_ru || "")}" /></label>
      <div class="action-row">
        <button class="ghost-btn icon-mini" data-builder-action="option-up" type="button">↑</button>
        <button class="ghost-btn icon-mini" data-builder-action="option-down" type="button">↓</button>
        <button class="danger-btn icon-mini" data-builder-action="remove-option" type="button">Өшіру</button>
      </div>
    </div>`;
  }

  function renumberBuilder(list) {
    [...list.querySelectorAll(".builder-question")].forEach((question, qi) => {
      question.querySelector("[data-question-number]").textContent = `Сұрақ ${qi + 1}`;
      question.querySelectorAll("[data-correct]").forEach((radio, oi) => {
        radio.name = `correct_${qi}`;
        radio.value = String(oi);
      });
    });
  }

  function collectTestBuilder(form) {
	    const fd = new FormData(form);
	    return {
	      lesson_id: fd.get("lesson_id"),
	      title: String(fd.get("title") || "").trim(),
	      pass_percent: Number(fd.get("pass_percent") || 70),
      is_active: fd.get("is_active") === "on",
      questions: [...form.querySelectorAll(".builder-question")].map((question, qi) => ({
        question_text_kk: question.querySelector("[data-question-kk]").value.trim(),
        question_text_ru: question.querySelector("[data-question-ru]").value.trim(),
        sort_order: qi + 1,
        options: [...question.querySelectorAll(".builder-option")].map((option, oi) => ({
          option_text_kk: option.querySelector("[data-option-kk]").value.trim(),
          option_text_ru: option.querySelector("[data-option-ru]").value.trim(),
          is_correct: Boolean(option.querySelector("[data-correct]").checked),
          sort_order: oi + 1,
        })),
      })),
    };
  }

	  function validateTestBuilder(test) {
	    if (!test.lesson_id || !test.title) return "Сабақ және тест атауы міндетті";
    if (test.pass_percent < 1 || test.pass_percent > 100) return "Өту пайызы 1–100 аралығында болуы керек";
    if (!test.questions.length) return "Кемінде 1 сұрақ қосыңыз";
    for (const question of test.questions) {
      if (!question.question_text_kk) return "Сұрақ мәтіні бос болмауы керек";
      if (question.options.length < 2) return "Әр сұрақта кемінде 2 жауап болуы керек";
      if (question.options.some((option) => !option.option_text_kk)) return "Жауап мәтіні бос болмауы керек";
      if (question.options.filter((option) => option.is_correct).length !== 1) return "Әр сұрақта дәл 1 дұрыс жауап белгіленуі керек";
    }
    return "";
  }

	  async function renderAdminPayments() {
	    const rows = await fetchAdminPayments();
	    els.adminContent.innerHTML = `
	      <div class="card">
	        <div class="admin-section-head">
	          <div><p class="eyebrow">Төлемдер</p><h2>Төлемдер</h2></div>
	        </div>
	        ${adminPaymentsTableHtml(rows)}
	      </div>
	      <div class="card">
	        <div class="section-head"><h2>Қолмен тексеру</h2></div>
	        <form id="paymentAction" class="form">
	          <div class="grid two">
	            <label class="field"><span>Төлем ID</span><input name="id" required placeholder="UUID" value="${esc(state.adminPaymentManualID)}" /></label>
	            <label class="field"><span>Пікір / override</span><input name="comment" placeholder="Қабылдамау себебі немесе override түсіндірмесі" value="${esc(state.adminPaymentManualComment)}" /></label>
	          </div>
	          <div class="action-row">
	            <button class="gold-btn" name="action" value="approve" type="submit">Қабылдау</button>
            <button class="danger-btn" name="action" value="reject" type="submit">Қабылдамау</button>
          </div>
        </form>
	      </div>
	    `;
	    const paymentForm = document.getElementById("paymentAction");
	    paymentForm.addEventListener("input", (event) => {
	      if (event.target.name === "id") state.adminPaymentManualID = event.target.value;
	      if (event.target.name === "comment") state.adminPaymentManualComment = event.target.value;
	    });
	    paymentForm.addEventListener("submit", async (event) => {
	      event.preventDefault();
	      const submitter = event.submitter;
	      const form = new FormData(event.currentTarget);
	      const id = form.get("id");
	      state.adminPaymentManualID = clean(id);
	      state.adminPaymentManualComment = clean(form.get("comment"));
	      try {
	        if (submitter.value === "approve") {
	          await api(`/api/admin/payments/${id}/approve`, {
	            method: "POST",
            body: JSON.stringify({ days: 30, override_comment: form.get("comment") }),
          });
          toast("Қабылданды", "success");
	        } else {
	          await api(`/api/admin/payments/${id}/reject`, {
	            method: "POST",
	            body: JSON.stringify({ comment: form.get("comment") }),
	          });
	          toast("Қабылданбады", "success");
	        }
	        await loadAdminPaymentsTable();
	      } catch (error) {
	        toast(error.message || "Әрекет орындалмады", "error");
	      }
	    });
	    delegate(els.adminContent, "[data-copy-payment-id]", "click", (event, target) => {
	      event.preventDefault();
	      copyText(target.dataset.copyPaymentId)
	        .then(() => toast("UUID көшірілді", "success"))
	        .catch(() => toast("UUID көшіру мүмкін болмады", "error"));
	    });
	    delegate(els.adminContent, "[data-use-payment-id]", "click", (event, target) => {
	      event.preventDefault();
	      const id = target.dataset.usePaymentId || "";
	      const input = document.querySelector("#paymentAction input[name=id]");
	      state.adminPaymentManualID = id;
	      if (input) {
	        input.value = id;
	        input.focus();
	      }
	      toast("UUID қолмен тексеруге қойылды", "success");
	    });
	    startAdminPolling("payments", loadAdminPaymentsTable);
	  }

	  async function fetchAdminPayments() {
	    if (state.adminPaymentsLoading) return state.adminPayments || [];
	    state.adminPaymentsLoading = true;
	    try {
	      const data = await api("/api/admin/payments");
	      state.adminPayments = data.payments || [];
	      return state.adminPayments;
	    } finally {
	      state.adminPaymentsLoading = false;
	    }
	  }

	  async function loadAdminPaymentsTable() {
	    const rows = await fetchAdminPayments();
	    const table = els.adminContent && els.adminContent.querySelector("[data-admin-payments-table]");
	    if (table) {
	      table.replaceWith(htmlToNode(adminPaymentsTableHtml(rows)));
	    }
	    return rows;
	  }

	  function adminPaymentsTableHtml(rows) {
	    if (!rows.length) return `<div data-admin-payments-table>${emptyState("Мәлімет табылмады")}</div>`;
	    return `<div class="table-wrap admin-payments-table-wrap" data-admin-payments-table><table class="admin-payments-table">
	      <thead><tr><th>ID</th><th>Қолданушы</th><th>Байланыс</th><th>Төлем түрі</th><th>Өнім</th><th>Сома</th><th>Статус</th><th>Чек валидациясы</th><th>Чек</th></tr></thead>
	      <tbody>${rows
	        .map((payment) => {
	          const receipt = payment.receipt || {};
	          const product = payment.payment_type === "premium_course"
	            ? payment.premium_course_title || payment.premium_course_slug || shortId(payment.premium_course_id)
	            : payment.tariff_code;
	          return `<tr>
	            <td>${paymentIDCell(payment)}</td>
	            <td>${esc(payment.user ? `${payment.user.first_name || ""} @${payment.user.username || ""}` : shortId(payment.user_id))}</td>
	            <td>${esc(payment.contact_phone || (payment.user && payment.user.phone) || "—")}</td>
	            <td>${esc(payment.payment_type || "subscription")}</td>
	            <td>${esc(product || "—")}</td>
	            <td>${money(payment.amount_kzt)} ₸</td>
	            <td>${statusBadge(payment.status)}${payment.admin_comment ? `<div class="muted small wrap-text">${esc(payment.admin_comment)}</div>` : ""}</td>
	            <td>${receipt.validation_status ? receiptValidationSummary(receipt, payment) : "—"}</td>
	            <td>${receiptOpenHtml(receipt)}</td>
	          </tr>`;
	        })
	        .join("")}</tbody>
	    </table></div>`;
	  }

	  function paymentIDCell(payment) {
	    const id = clean(payment && payment.id);
	    return `<div class="payment-id-cell">
	      <code class="mono-id full">${esc(id || "—")}</code>
	      <div class="action-row payment-id-actions">
	        <button class="ghost-btn icon-mini" data-copy-payment-id="${esc(id)}" type="button">Copy UUID</button>
	        <button class="ghost-btn icon-mini" data-use-payment-id="${esc(id)}" type="button">Қолмен тексеру</button>
	      </div>
	    </div>`;
	  }

	  function receiptOpenHtml(receipt) {
	    const path = compact(receipt && receipt.file_path);
	    if (!path) return `<button class="ghost-btn receipt-open-btn" type="button" disabled>Чек жоқ</button>`;
	    return `<a class="ghost-btn receipt-open-btn" href="${esc(path)}" target="_blank" rel="noopener">Чекті ашу</a>`;
	  }

	  function receiptValidationSummary(receipt, payment) {
	    const errors = receipt.validation_errors || [];
	    const diff = typeof receipt.amount_difference_kzt === "number"
      ? receipt.amount_difference_kzt
      : typeof receipt.parsed_amount_kzt === "number"
	        ? receipt.parsed_amount_kzt - payment.amount_kzt
	        : null;
	    const approved = receipt.validation_status === "approved";
	    const identity = receipt.parsed_check_id || receipt.receipt_transaction_key || receipt.parsed_transaction_id || "—";
	    return `<div class="receipt-validation">
	      ${statusBadge(receipt.validation_status)}
	      ${validationDetail("Байланыс", payment.contact_phone || "—")}
	      ${validationDetail("Күтілетін сома", `${money(payment.amount_kzt)} ₸`)}
	      ${validationDetail("Рұқсат етілген ауытқу", typeof receipt.amount_tolerance_kzt === "number" ? `${money(receipt.amount_tolerance_kzt)} ₸` : "—")}
	      ${validationDetail("Чектегі сома", typeof receipt.parsed_amount_kzt === "number" ? `${money(receipt.parsed_amount_kzt)} ₸` : "—")}
	      ${validationDetail("Айырма", diff === null ? "—" : `${money(diff)} ₸`)}
	      ${validationDetail("БИН/ИИН", receipt.parsed_recipient_bin || "—", true)}
	      ${validationDetail("Күтілетін БИН/ИИН", receipt.expected_recipient_bin || "—", true)}
	      ${validationDetail("Валюта", receipt.parsed_currency || "—")}
	      ${validationDetail("Чек/QR", identity, true)}
	      ${validationDetail("Провайдер", receipt.provider || "unknown")}
	      ${validationDetail("QR", receipt.qr_found ? "QR табылды" : "QR табылмады")}
	      ${validationDetail("Файл hash", receipt.file_hash || "—", true)}
	      ${validationDetail("Text hash", receipt.raw_text_hash || "—", true)}
	      ${validationDetail("Қайталану", receipt.duplicate_of_receipt_id ? `Қайталанған: ${receipt.duplicate_of_receipt_id}` : "Бірегей", Boolean(receipt.duplicate_of_receipt_id))}
	      ${!approved && errors.length ? `<small>${errors.map(receiptErrorText).map(esc).join(", ")}</small>` : ""}
	    </div>`;
	  }

	  function validationDetail(label, value, mono) {
	    const tag = mono ? "code" : "strong";
	    return `<span><b>${esc(label)}:</b> <${tag}>${esc(value)}</${tag}></span>`;
	  }

	  function receiptErrorText(code) {
	    const labels = {
	      amount_mismatch: "Сома рұқсат етілген ауытқудан тыс",
	      amount_not_found: "Чектегі сома оқылмады",
	      currency_missing: "Валюта табылмады",
	      currency_mismatch: "Валюта KZT емес",
	      recipient_bin_missing: "БИН/ИИН табылмады",
	      recipient_bin_mismatch: "БИН/ИИН сәйкес емес",
	      recipient_bin_not_configured: "Күтілетін БИН/ИИН бапталмаған",
	      provider_marker_missing: "Провайдер белгісі табылмады",
	      payment_date_too_early: "Чек төлем жасалған уақыттан ертерек",
	      strong_identity_not_found: "Чек/QR нөмірі табылмады",
	      duplicate_identity_found: "Қайталанған чек белгісі табылды",
	      pdf_text_unreadable: "PDF мәтіні оқылмады",
	      file_read_failed: "Файл оқылмады",
	    };
	    return labels[code] || String(code || "");
	  }

  async function renderAdminChannels() {
    const [levels, data] = await Promise.all([adminLevels(), api("/api/admin/channels")]);
    const rows = data.channels || [];
    els.adminContent.innerHTML = `
      <div class="card">
        <div class="admin-section-head">
          <div><p class="eyebrow">Каналдар</p><h2>Жабық каналдар</h2></div>
          <div class="admin-toolbar">
            <button class="gold-btn" id="addChannel" type="button">+ Канал қосу</button>
          </div>
        </div>
        ${channelTableHtml(rows)}
      </div>
    `;
    on("addChannel", () => openChannelModal(null, levels));
    delegate(els.adminContent, "[data-edit-channel]", "click", (event, target) => {
      event.stopPropagation();
      const channel = rows.find((item) => item.id === target.dataset.editChannel);
      if (channel) openChannelModal(channel, levels);
    });
    delegate(els.adminContent, "[data-delete-channel]", "click", async (event, target) => {
      event.stopPropagation();
      const channel = rows.find((item) => item.id === target.dataset.deleteChannel);
      const ok = await confirmAction({
        title: "Каналды өшіру",
        body: `<p class="muted">${esc(channel ? channel.title : "Бұл канал")} белсенді емес күйге ауысады. Жалғастырасыз ба?</p>`,
        confirmLabel: "Өшіру",
        action: () => api(`/api/admin/channels/${target.dataset.deleteChannel}`, { method: "DELETE" }),
        successMessage: "Канал белсенді емес күйге ауысты",
        errorMessage: "Каналды өшіру мүмкін болмады",
      });
      if (ok) renderAdminChannels();
    });
  }

	  function channelTableHtml(channels) {
	    if (!channels.length) return emptyState("Каналдар табылмады");
	    return `<div class="table-wrap"><table>
	      <thead><tr>
	        <th>ID</th><th>Атауы</th><th>Telegram chat ID</th><th>Шақыру сілтемесі</th><th>Тариф</th><th>Деңгей</th><th>Статус</th><th>Әрекет</th>
	      </tr></thead>
	      <tbody>${channels
	        .map((channel) => {
	          const link = compact(channel.manual_invite_link);
	          return `<tr>
	            <td>${esc(shortId(channel.id))}</td>
	            <td><strong>${esc(channel.title)}</strong></td>
	            <td>${esc(channel.telegram_chat_id)}</td>
	            <td>${link ? `<a class="link" href="${esc(link)}" target="_blank" rel="noopener">${esc(link)}</a>` : esc(channel.invite_link_type || "bot")}</td>
	            <td>${esc(channel.tariff_requirement)}</td>
	            <td>Деңгей ${esc(channel.level_requirement)}</td>
	            <td>${statusBadge(channel.is_active ? "active" : "inactive")}</td>
	            <td><div class="action-row">
	              <button class="ghost-btn" data-edit-channel="${esc(channel.id)}" type="button">Өзгерту</button>
	              <button class="danger-btn" data-delete-channel="${esc(channel.id)}" type="button">Өшіру</button>
	            </div></td>
	          </tr>`;
	        })
	        .join("")}</tbody>
	    </table></div>`;
	  }

	  function openChannelModal(channel, levels) {
	    const isEdit = Boolean(channel && channel.id);
	    const active = !isEdit || channel.is_active;
	    const inviteType = (channel && channel.invite_link_type) || (channel && channel.manual_invite_link ? "manual" : "bot");
	    const tariff = (channel && channel.tariff_requirement) || "BASIC";
	    const level = (channel && channel.level_requirement) || 1;
	    const shell = openModalShell(isEdit ? "Каналды өзгерту" : "Канал қосу", `
	      <form id="channelModalForm" class="form">
	        <div class="grid two">
	          <label class="field"><span>Атауы</span><input name="title" required value="${esc((channel && channel.title) || "")}" /></label>
	          <label class="field"><span>Telegram chat ID</span><input name="telegram_chat_id" required value="${esc((channel && channel.telegram_chat_id) || "")}" /></label>
	        </div>
	        <div class="grid two">
	          <label class="field"><span>Шақыру түрі</span><select name="invite_link_type">
	            <option value="bot" ${inviteType === "bot" ? "selected" : ""}>Бот арқылы</option>
	            <option value="manual" ${inviteType === "manual" ? "selected" : ""}>Қолмен сілтеме</option>
	          </select></label>
	          <label class="field"><span>Қолмен сілтеме</span><input name="manual_invite_link" placeholder="${esc(DEFAULT_CHANNEL_LINK)}" value="${esc((channel && channel.manual_invite_link) || "")}" /></label>
	        </div>
	        <div class="grid two">
	          <label class="field"><span>Тариф талабы</span><select name="tariff_requirement">
	            ${["BASIC", "STANDARD", "VIP"].map((item) => `<option value="${item}" ${tariff === item ? "selected" : ""}>${item}</option>`).join("")}
	          </select></label>
	          <label class="field"><span>Деңгей талабы</span><select name="level_requirement">${levelNumberOptions(levels, level)}</select></label>
	        </div>
	        <label class="switch-field"><input name="is_active" type="checkbox" ${active ? "checked" : ""} /><span>Белсенді</span></label>
	        <div class="action-row end">
	          <button class="ghost-btn" data-close-modal type="button">Болдырмау</button>
	          <button class="gold-btn" type="submit"><span class="btn-label">Сақтау</span><span class="btn-spinner"></span></button>
	        </div>
	      </form>
	    `);
	    const form = shell.body.querySelector("form");
	    shell.body.querySelector("[data-close-modal]").addEventListener("click", shell.close);
	    form.addEventListener("submit", async (event) => {
	      event.preventDefault();
	      const btn = form.querySelector("button[type=submit]");
	      if (buttonIsLoading(btn)) return;
	      const body = Object.fromEntries(new FormData(form).entries());
	      body.title = compact(body.title);
	      body.telegram_chat_id = compact(body.telegram_chat_id);
	      body.manual_invite_link = compact(body.manual_invite_link);
	      body.level_requirement = Number(body.level_requirement || 1);
	      body.is_active = new FormData(form).get("is_active") === "on";
	      if (!body.title || !body.telegram_chat_id) {
	        toast("Атауы және Telegram chat ID міндетті", "error");
	        return;
	      }
	      if (body.invite_link_type === "manual" && !body.manual_invite_link) {
	        toast("Қолмен сілтеме жазыңыз немесе бот арқылы түрін таңдаңыз", "error");
	        return;
	      }
	      if (body.manual_invite_link && !isValidTelegramLink(body.manual_invite_link)) {
	        toast("Telegram link https://t.me/... форматында болуы керек", "error");
	        return;
	      }
	      try {
	        setButtonLoading(btn, true);
	        setModalBusy(shell, true);
	        await api(isEdit ? `/api/admin/channels/${channel.id}` : "/api/admin/channels", {
	          method: isEdit ? "PATCH" : "POST",
	          body: JSON.stringify(body),
	        });
	        shell.close();
	        toast("Канал сақталды", "success");
	        renderAdminChannels();
	      } catch (error) {
	        toast(error.message || "Сақтау сәтсіз", "error");
	      } finally {
	        setModalBusy(shell, false);
	        setButtonLoading(btn, false);
	      }
	    });
	  }

  async function renderAdminBroadcast() {
    const data = await api("/api/admin/broadcasts").catch(() => ({ broadcasts: [] }));
    const broadcasts = data.broadcasts || [];
    els.adminContent.innerHTML = `
      <div class="card">
        <div class="section-head">
          <div><p class="eyebrow">Хабарлама</p><h2>Хабарлама жіберу</h2></div>
        </div>
        <form id="broadcastForm" class="form">
          <label class="field"><span>Тақырып</span><input name="title" placeholder="Қосымша" /></label>
          <label class="field"><span>Хабарлама мәтіні</span><textarea name="body" required></textarea></label>
          <label class="field"><span>Кімге жіберіледі</span><select name="target"><option value="all">Барлық қолданушылар</option><option value="active">Белсенді қолданушылар</option><option value="inactive">Белсенді емес қолданушылар</option></select></label>
          <button class="gold-btn" type="submit">Жіберу</button>
        </form>
      </div>
      <div class="card">
        <div class="section-head"><h2>Хабарлама тарихы</h2></div>
        ${
          broadcasts.length
            ? `<div class="table-wrap"><table><thead><tr><th>ID</th><th>Статус</th><th>Кімге</th><th>Жіберілді</th><th>Қате</th><th>Құрылған уақыты</th></tr></thead><tbody>${broadcasts
                .map(
                  (item) => `<tr><td>${esc(shortId(item.id))}</td><td>${statusBadge(item.status)}</td><td>${esc(targetLabel(item.target))}</td><td>${esc(item.sent_count || 0)}</td><td>${esc(item.failed_count || 0)}</td><td>${formatDateTime(item.created_at)}</td></tr>`,
                )
                .join("")}</tbody></table></div>`
            : emptyState("Мәлімет табылмады")
        }
      </div>
    `;
    document.getElementById("broadcastForm").addEventListener("submit", async (event) => {
      event.preventDefault();
      try {
        const body = Object.fromEntries(new FormData(event.currentTarget).entries());
        await api("/api/admin/broadcast", { method: "POST", body: JSON.stringify(body) });
        toast("Хабарлама кезекке қосылды", "success");
        renderAdminBroadcast();
      } catch (error) {
        toast(error.message || "Хабарлама жіберілмеді", "error");
      }
    });
  }

  function targetLabel(target) {
    return target === "active" ? "Белсенді қолданушылар" : target === "inactive" ? "Белсенді емес қолданушылар" : "Барлық қолданушылар";
  }

  async function renderAdminSettings() {
    const data = await api("/api/admin/settings");
    const settings = data.settings || {};
    const channelLink = compact(settings.channel_link) || DEFAULT_CHANNEL_LINK;
    els.adminContent.innerHTML = `
      <div class="card">
        <div class="section-head"><h2>Баптаулар</h2></div>
        <form id="settingsForm" class="form">
          <label class="field"><span>Канал / бот сілтемесі</span><input name="channel_link" required placeholder="${esc(DEFAULT_CHANNEL_LINK)}" value="${esc(channelLink)}" /></label>
          <button class="gold-btn" type="submit"><span class="btn-label">Сақтау</span><span class="btn-spinner"></span></button>
        </form>
      </div>
      <div class="card">
        <div class="section-head"><h2>Ағымдағы мәндер</h2></div>
        <pre style="overflow:auto;background:rgba(8,6,4,0.6);padding:14px;border-radius:12px;border:1px solid var(--border-soft);font-size:12px;color:var(--text-soft)">${esc(JSON.stringify(settings, null, 2))}</pre>
      </div>
    `;
    document.getElementById("settingsForm").addEventListener("submit", async (event) => {
      event.preventDefault();
      const btn = event.currentTarget.querySelector("button[type=submit]");
      if (buttonIsLoading(btn)) return;
      const channelLink = compact(new FormData(event.currentTarget).get("channel_link"));
      if (!isValidTelegramLink(channelLink)) {
        toast("Telegram link https://t.me/... форматында болуы керек", "error");
        return;
      }
      setButtonLoading(btn, true);
      try {
        await api("/api/admin/settings", {
          method: "PATCH",
          body: JSON.stringify({ channel_link: channelLink }),
        });
        toast("Баптаулар сақталды", "success");
        renderAdminSettings();
      } catch (error) {
        toast(error.message || "Баптауларды сақтау мүмкін болмады", "error");
      } finally {
        setButtonLoading(btn, false);
      }
    });
  }

  /* ===========================================================
     ADMIN TABLE HELPER
     =========================================================== */

  function tableHtml(columns, rows) {
    if (!rows || !rows.length) {
      return emptyState("Мәлімет табылмады", "Ø");
    }
    return `<div class="table-wrap"><table>
      <thead><tr>${columns.map((c) => `<th>${esc(adminLabel(c))}</th>`).join("")}</tr></thead>
      <tbody>${rows
        .map(
          (row) =>
            `<tr>${columns.map((c) => `<td>${esc(formatCell(row[c]))}</td>`).join("")}</tr>`,
        )
        .join("")}</tbody>
    </table></div>`;
  }

  function formatCell(value) {
    if (value === null || value === undefined || value === "") return "—";
    if (typeof value === "boolean") return value ? "Белсенді" : "Жабық";
    if (typeof value === "object") return JSON.stringify(value);
    if (typeof value === "string" && /^[0-9a-f]{8}-[0-9a-f-]{27}$/i.test(value)) return shortId(value);
    if (typeof value === "string" && /^\d{4}-\d{2}-\d{2}T/.test(value)) return formatDateTime(value);
    return String(value);
  }

  function htmlToNode(markup) {
    const tmp = document.createElement("div");
    tmp.innerHTML = markup;
    return tmp.firstElementChild;
  }

  function debounce(fn, wait) {
    let t;
    return function (...args) {
      clearTimeout(t);
      t = setTimeout(() => fn.apply(this, args), wait);
    };
  }

  /* ===========================================================
     KICKOFF
     =========================================================== */

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", boot, { once: true });
  } else {
    boot();
  }
})();
