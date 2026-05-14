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
    referral: null,
    coins: null,
    currentScreen: "dashboard",
    selectedTariff: null,
    financialIqAnswers: {},
    financialIqResult: null,
    financialIqReturnScreen: "dashboard",
    adminScreen: "dashboard",
    admin: null,
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
    try {
      if (typeof webApp.setBackgroundColor === "function") webApp.setBackgroundColor("#07090E");
      if (typeof webApp.setHeaderColor === "function") webApp.setHeaderColor("#07090E");
    } catch (_) {}
    try {
      if (typeof webApp.disableVerticalSwipes === "function") webApp.disableVerticalSwipes();
    } catch (_) {}

    if (typeof webApp.requestFullscreen === "function" && !webApp.isFullscreen) {
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

	  function visibleTariffImage(tariff) {
	    if (!tariff) return "";
	    return compact(tariff.image_file_path) || compact(tariff.image_url);
	  }

	  function selectedTariff() {
	    const tariffs = (state.platform && state.platform.tariffs) || [];
	    const selected = compact(state.selectedTariff);
	    return tariffs.find((item) => item.id === selected || item.code === selected) || tariffs[0] || null;
	  }

	  function copyText(value) {
	    const text = clean(value);
	    if (navigator.clipboard && navigator.clipboard.writeText) {
	      return navigator.clipboard.writeText(text);
	    }
	    const input = document.createElement("textarea");
	    input.value = text;
	    input.setAttribute("readonly", "readonly");
	    input.style.position = "fixed";
	    input.style.opacity = "0";
	    document.body.appendChild(input);
	    input.select();
	    const ok = document.execCommand("copy");
	    input.remove();
	    return ok ? Promise.resolve() : Promise.reject(new Error("copy failed"));
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
	    short_description_kk: "Қысқа сипаттама",
	    full_description_kk: "Толық сипаттама",
	    image_url: "Сурет URL",
	    image_file_path: "Жүктелген сурет",
	    amount_kzt: "Сома",
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
    valid_candidate: "Тексеруге дайын",
    parse_partial: "Қолмен тексеру қажет",
    parse_failed: "Қолмен тексеру қажет",
    suspicious: "Күдікті түбіртек",
    duplicate: "Қайталанған түбіртек",
    uploaded: "Жүктелді",
    sent: "Жіберілді",
  };

  function statusBadge(value) {
    const raw = String(value == null ? "" : value);
    const label = statusText[raw] || (raw === "true" ? "Белсенді" : raw === "false" ? "Жабық" : raw || "—");
    const kind =
      ["active", "approved", "completed", "sent", "valid_candidate", "true"].includes(raw)
        ? "ok"
        : ["rejected", "expired", "cancelled", "failed", "duplicate", "false"].includes(raw)
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
    const [me, platform, levels] = await Promise.all([
      api("/api/me"),
      api("/api/platform").catch(() => null),
      api("/api/levels").catch(() => ({ levels: [] })),
    ]);
    state.me = me;
    state.platform = platform;
    state.levels = (levels && levels.levels) || [];

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
      financialIq: renderFinancialIq,
      financialIqResult: renderFinancialIqResult,
      levels: renderLevels,
      lessons: renderLessons,
      test: renderTest,
      assignment: renderAssignment,
      referral: renderReferral,
      coins: renderCoins,
      streams: renderStreams,
      channels: renderChannels,
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
	    if (state.currentScreen === "financialIq" || state.currentScreen === "financialIqResult") {
	      returnFromFinancialIq();
	    }
	  }

	  function syncTelegramBackButton() {
	    const tg = getTelegram();
	    if (!tg || !tg.BackButton) return;
	    try {
	      const hasBack = state.currentScreen === "payment" || state.currentScreen === "financialIq" || state.currentScreen === "financialIqResult";
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
	    return `<article class="card financial-iq-card">
	      <div>
	        <p class="eyebrow">Тест</p>
	        <h2>Қаржылық IQ тесті</h2>
	        <p class="muted">33 сұраққа жауап беріп, қаржылық деңгейіңізді анықтаңыз.</p>
	      </div>
	      <button class="gold-btn" data-financial-iq type="button">Тесттен өту</button>
	    </article>`;
	  }

	  function bindFinancialIqCta() {
	    document.querySelectorAll("[data-financial-iq]").forEach((button) => {
	      button.addEventListener("click", openFinancialIq);
	    });
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
        ${financialIqCtaCard()}
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
        <div class="card">
          <p class="eyebrow">Premium жабық клуб</p>
          <h2>Жабық мүшелік</h2>
          <p class="muted">Тек тариф ашқан клиенттер ғана сабақтарға, тестке, эфирге және жеке арналарға қол жеткізе алады.</p>
        </div>
      </section>
    `);
    on("goDiagnostics", () => setScreen("diagnostics"));
    on("goTariffs", () => setScreen("tariffs"));
    bindFinancialIqCta();
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
        ${financialIqCtaCard()}
        <div class="hero">
	          <p class="eyebrow">Жүйелі даму платформасы</p>
	          <h1>ZHENIS ORDA UNIVERSE</h1>
	          <p class="muted">${esc(progress.next_requirement || "Ойлау, қаржы, бизнес және лидерлік бойынша жүйелі дамыңыз.")}</p>
	          <div class="progress-wrap">
	            <div class="progress-track"><div class="progress-fill" style="--progress:${percent}%"></div></div>
	            <div class="certificate-goal ${percent >= 100 ? "active" : ""}" title="Сертификат"><span class="certificate-icon" aria-hidden="true"></span></div>
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
          <button class="ghost-btn lg" data-next="assignment" type="button">Тапсырмаларым</button>
          <button class="ghost-btn lg" data-next="support" type="button">Қолдау қызметі</button>
        </div>
      </section>
    `);
    bindNext();
    bindFinancialIqCta();
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
    </article>`;
  }

  async function renderLessons() {
    const level = (state.me && state.me.user && state.me.user.current_level) || 1;
    html(`<section class="screen">
      <div class="section-head">
	        <div><p class="eyebrow">Деңгей ${esc(level)}</p><h1>Сабақтар</h1></div>
        <button class="ghost-btn" id="refreshLessons" type="button">Жаңарту</button>
      </div>
      <div class="grid">${skeletonRows(3)}</div>
    </section>`);

    let data;
    try {
      data = await api(`/api/lessons?level=${level}`);
    } catch (error) {
      html(`<section class="screen"><h1>Сабақтарым</h1>${emptyState(error.message)}</section>`);
      return;
    }
    state.lessons = data.lessons || [];

    const cards = state.lessons.length
      ? state.lessons.map(lessonCard).join("")
      : emptyState("Бұл деңгейге сабақтар әлі қосылған жоқ.");

    html(`
      <section class="screen">
        <div class="section-head">
	          <div><p class="eyebrow">Деңгей ${esc(level)}</p><h1>Сабақтар</h1></div>
          <button class="ghost-btn" id="refreshLessons" type="button">Жаңарту</button>
        </div>
        <div class="grid">${cards}</div>
      </section>
    `);
    on("refreshLessons", renderLessons);
    document
      .querySelectorAll("[data-watch]")
      .forEach((btn) => btn.addEventListener("click", () => markWatched(btn.dataset.watch)));
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
        <button class="${watched ? "ghost-btn" : "gold-btn"}" data-watch="${lesson.id}" ${access ? "" : "disabled"} type="button">
          ${watched ? "Қайта белгілеу" : "Сабақты өттім"}
        </button>
      </div>
    </article>`;
  }

  async function markWatched(id) {
    try {
      await api(`/api/lessons/${id}/watched`, { method: "POST", body: "{}" });
      const me = await api("/api/me");
      state.me = me;
      await refreshLevels();
      toast("Сабақ белгіленді", "success");
      renderLessons();
    } catch (error) {
      toast(error.message || "Жаңарту мүмкін болмады", "error");
    }
  }

  async function renderTest() {
    const level = (state.me && state.me.user && state.me.user.current_level) || 1;
    let data;
    try {
      data = await api(`/api/tests/${level}`);
    } catch (error) {
      html(`<section class="screen"><div class="section-head"><h1>Тест</h1></div>${emptyState(error.message || "Тест әлі ашылмаған")}</section>`);
      return;
    }
    const test = data.test;
    if (!test) {
      html(`<section class="screen"><h1>Тест</h1>${emptyState("Бұл деңгейге тест әлі қосылған жоқ.")}</section>`);
      return;
    }
    html(`
      <section class="screen">
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
        const me = await api("/api/me");
        state.me = me;
        await refreshLevels();
        toast(`Балл: ${result.attempt.score_percent}% — ${result.attempt.passed ? "Сәтті" : "Қайталау"}`, result.attempt.passed ? "success" : "error");
        setScreen("dashboard");
      } catch (error) {
        toast(error.message || "Тест жіберу мүмкін болмады", "error");
      } finally {
        setButtonLoading(submitBtn, false);
      }
    });
  }

  function questionBlock(question) {
    return `<fieldset class="card">
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
	    document.getElementById("financialIqForm").addEventListener("submit", (event) => {
	      event.preventDefault();
	      calculateFinancialIq(event.currentTarget);
	      setScreen("financialIqResult");
	    });
	  }

	  function renderFinancialIqResult() {
	    const result = state.financialIqResult;
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
	      <button class="gold-btn block" data-tariff="${esc(tariff.id || tariff.code)}" type="button">Таңдау</button>
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
	    const providers = (state.platform && state.platform.providers) || [
	      { code: "kaspi_qr", title: "Kaspi QR" },
	      { code: "kaspi_pay", title: "Kaspi Pay" },
	      { code: "halyk", title: "Halyk" },
	      { code: "bank_card", title: "Банк картасы" },
	    ];
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
	          <p class="muted">Kaspi QR / Kaspi Pay арқылы төлем жасап, түбіртекті Telegram ботқа PDF немесе сурет ретінде жіберіңіз.</p>
	        </div>
	        <form id="paymentForm" class="form">
	          <label class="field">
	            <span>Төлем провайдері</span>
	            <select name="provider">${providers.map((provider) => `<option value="${esc(provider.code)}">${esc(provider.title)}</option>`).join("")}</select>
	          </label>
	          <button class="gold-btn lg" type="submit"><span class="btn-label">Төлем жасау</span><span class="btn-spinner"></span></button>
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
	        const provider = new FormData(event.currentTarget).get("provider");
	        const res = await api("/api/payments", {
	          method: "POST",
	          body: JSON.stringify({ tariff_id: tariff.id, tariff_code: tariff.code, provider }),
	        });
	        document.getElementById("paymentResult").innerHTML = `
	          <div class="card">
	            <p class="eyebrow">Төлем құрылды</p>
	            <h2>Төлем #${esc(shortId(res.payment.id))}</h2>
            <p class="muted">${esc(res.instructions.text)}</p>
            <p>Сома: <strong>${money(res.payment.amount_kzt)} ₸</strong></p>
          </div>
          ${receiptUploadHtml(res.payment)}
        `;
	        bindReceiptUpload(res.payment.id);
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
            <h2>PDF немесе сурет жүктеңіз</h2>
          </div>
          <span class="status warn">Қолмен тексеріледі</span>
        </div>
        <form id="receiptUploadForm" class="form">
          <label class="upload-drop">
            <input name="receipt" type="file" accept=".pdf,.jpg,.jpeg,.png,.webp,application/pdf,image/*" required />
            <span class="upload-title">Түбіртек жүктеу</span>
            <small>PDF, JPG, PNG немесе WEBP</small>
            <strong id="receiptFileName">Файл таңдалмады</strong>
          </label>
          <button class="gold-btn lg" type="submit">
            <span class="btn-label">Тексеруге жіберу</span><span class="btn-spinner"></span>
          </button>
        </form>
        <div id="receiptUploadState" class="muted small">Әкімші тексергеннен кейін қолжетімділік ашылады.</div>
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
        </div>
        <div class="grid two">
	          <button class="ghost-btn" data-next="referral" type="button">Дос шақыру</button>
          <button class="ghost-btn" data-next="support" type="button">Қолдау қызметі</button>
        </div>
      </section>
    `);
    bindNext();
  }

  function renderSupport() {
    html(`
      <section class="screen">
        <div class="card">
          <p class="eyebrow">Қолдау</p>
          <h1>Қолдау қызметі</h1>
          <p class="muted">Сұрағыңызды жазыңыз. Команда жауап береді.</p>
        </div>
        <form id="supportForm" class="form">
          <label class="field"><span>Хабарлама</span><textarea name="body" required placeholder="Хабарлама..."></textarea></label>
          <button class="gold-btn lg" type="submit"><span class="btn-label">Жіберу</span><span class="btn-spinner"></span></button>
        </form>
      </section>
    `);
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
      button.addEventListener("click", () => setScreen(button.dataset.next));
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
    const params = new URLSearchParams();
    const data = await api(`/api/admin/users?${params.toString()}`);
    const users = data.users || [];
    els.adminContent.innerHTML = `
      <div class="card">
        <div class="admin-section-head">
          <div><p class="eyebrow">Қолданушылар</p><h2>Қолданушылар</h2></div>
          <div class="admin-toolbar">
            <input id="userSearch" placeholder="Аты немесе username бойынша іздеу" />
          </div>
        </div>
        ${tableHtml(["id", "telegram_id", "username", "first_name", "current_level", "coin_balance", "access_closed"], users)}
      </div>
    `;
    on("userSearch", debounce(async (event) => {
      const q = event.target.value;
      try {
        const data = await api(`/api/admin/users?q=${encodeURIComponent(q)}`);
        document.querySelector(".table-wrap")?.replaceWith(htmlToNode(tableHtml(["id", "telegram_id", "username", "first_name", "current_level", "coin_balance", "access_closed"], data.users || [])));
      } catch (error) {
        toast(error.message, "error");
      }
    }, 280), "input");
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
        <th>Реті</th><th>Қазақша атауы</th><th>Сипаттама</th><th>Статус</th><th>Әрекет</th>
      </tr></thead>
      <tbody>${levels
        .map(
          (level) => {
            const seeded = isSeededLevel(level);
            return `<tr>
	            <td><strong>Деңгей ${esc(level.number || "—")}</strong><div class="muted small">ID ${esc(shortId(level.id))}</div></td>
            <td><strong>${esc(level.title_kk || "—")}</strong></td>
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
    const shell = openModalShell(isEdit ? "Деңгейді жаңарту" : "Деңгей қосу", `
      <form id="levelModalForm" class="form">
        <div class="grid two">
          <label class="field"><span>Деңгей нөмірі / реті</span><input name="number" type="number" min="1" step="1" required ${seeded ? "readonly" : ""} value="${esc(number)}" /></label>
          <label class="switch-field"><input name="is_active" type="checkbox" ${active ? "checked" : ""} ${seeded ? "disabled" : ""} /><span>Белсенді</span></label>
        </div>
        ${seeded ? `<p class="muted small">Негізгі 12 деңгей жүйеде әрқашан белсенді сақталады.</p>` : ""}
        <label class="field"><span>Қазақша атауы</span><input name="title_kk" required value="${esc((level && level.title_kk) || "")}" /></label>
        <label class="field"><span>Сипаттама</span><textarea name="description_kk">${esc((level && level.description_kk) || "")}</textarea></label>
        <div class="action-row end">
          <button class="ghost-btn" data-close-modal type="button">Бас тарту</button>
          <button class="gold-btn" type="submit"><span class="btn-label">${isEdit ? "Жаңарту" : "Сақтау"}</span><span class="btn-spinner"></span></button>
        </div>
      </form>
    `);
    const form = shell.body.querySelector("form");
    shell.body.querySelector("[data-close-modal]").addEventListener("click", shell.close);
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
      setModalBusy(shell, true);
      try {
        await api(isEdit ? `/api/admin/levels/${level.id}` : "/api/admin/levels", {
          method: isEdit ? "PATCH" : "POST",
          body: JSON.stringify(payload),
        });
        shell.close();
        toast(isEdit ? "Деңгей жаңартылды" : "Деңгей қосылды", "success");
        renderAdminLevels();
      } catch (error) {
        toast(levelSaveErrorMessage(error), "error");
      } finally {
        setModalBusy(shell, false);
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
      is_active: seeded ? true : fd.get("is_active") === "on",
    };
  }

  function validateLevelForm(level, levels, existing) {
    if (!Number.isInteger(level.number) || level.number < 1) return "Деңгей нөмірі 1 немесе одан жоғары болуы керек";
    if (!level.title_kk) return "Қазақша атауы міндетті";
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
    const shell = openModalShell(isEdit ? "Сабақты өзгерту" : "Сабақ қосу", `
      <form id="lessonModalForm" class="form">
        <label class="field"><span>Деңгей</span><select name="level_id" required>${levelOptions(levels, selectedLevel)}</select></label>
        <div class="grid two">
          <label class="field"><span>Қазақша атауы</span><input name="title_kk" required value="${esc((lesson && lesson.title_kk) || "")}" /></label>
          <label class="field"><span>Орысша атауы</span><input name="title_ru" value="${esc((lesson && lesson.title_ru) || "")}" /></label>
        </div>
        <label class="field"><span>Сабақ сілтемесі</span><input name="video_url" required placeholder="Telegram post немесе video URL" value="${esc((lesson && lesson.video_url) || "")}" /></label>
        <div class="grid two">
          <label class="field"><span>Сипаттама KK</span><textarea name="description_kk">${esc((lesson && lesson.description_kk) || "")}</textarea></label>
          <label class="field"><span>Сипаттама RU</span><textarea name="description_ru">${esc((lesson && lesson.description_ru) || "")}</textarea></label>
        </div>
        <div class="grid two">
          <label class="field"><span>Реті</span><input name="sort_order" type="number" min="1" value="${esc((lesson && lesson.sort_order) || 1)}" /></label>
          <label class="switch-field"><input name="is_active" type="checkbox" ${!lesson || lesson.is_active ? "checked" : ""} /><span>Белсенді</span></label>
        </div>
        <div class="action-row end">
          <button class="ghost-btn" data-close-modal type="button">Болдырмау</button>
          <button class="gold-btn" type="submit"><span class="btn-label">Сақтау</span><span class="btn-spinner"></span></button>
        </div>
      </form>
    `);
    shell.body.querySelector("[data-close-modal]").addEventListener("click", shell.close);
    shell.body.querySelector("form").addEventListener("submit", async (event) => {
      event.preventDefault();
      const btn = event.currentTarget.querySelector("button[type=submit]");
      const form = new FormData(event.currentTarget);
      const body = Object.fromEntries(form.entries());
      body.sort_order = Number(body.sort_order || 1);
      body.is_active = form.get("is_active") === "on";
      if (!body.level_id || !body.title_kk.trim() || !body.video_url.trim()) {
        toast("Деңгей, сабақ атауы және сабақ сілтемесі міндетті", "error");
        return;
      }
      if (buttonIsLoading(btn)) return;
      setButtonLoading(btn, true);
      setModalBusy(shell, true);
      try {
        const url = isEdit ? `/api/admin/lessons/${lesson.id}` : "/api/admin/lessons";
        await api(url, { method: isEdit ? "PATCH" : "POST", body: JSON.stringify(body) });
        shell.close();
        toast("Сабақ сақталды", "success");
        renderAdminLessons();
      } catch (error) {
        toast(error.message || "Сақтау мүмкін болмады", "error");
      } finally {
        setModalBusy(shell, false);
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
      if (!question.querySelector("[data-correct]:checked")) {
        const first = question.querySelector("[data-correct]");
        if (first) first.checked = true;
      }
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
    const data = await api("/api/admin/payments");
    const rows = data.payments || [];
    const paymentRows = rows.length
      ? `<div class="table-wrap"><table>
          <thead><tr><th>ID</th><th>Қолданушы</th><th>Тариф</th><th>Сома</th><th>Статус</th><th>Чек валидациясы</th><th>Чек</th></tr></thead>
          <tbody>${rows
            .map((payment) => {
              const receipt = payment.receipt || {};
              return `<tr>
                <td>${esc(shortId(payment.id))}</td>
                <td>${esc(payment.user ? `${payment.user.first_name || ""} @${payment.user.username || ""}` : shortId(payment.user_id))}</td>
                <td>${esc(payment.tariff_code)}</td>
                <td>${money(payment.amount_kzt)} ₸</td>
                <td>${statusBadge(payment.status)}</td>
                <td>${receipt.validation_status ? receiptValidationSummary(receipt, payment) : "—"}</td>
                <td>${receipt.file_path ? `<a class="link" href="${esc(receipt.file_path)}" target="_blank" rel="noopener">Ашу</a>` : "—"}</td>
              </tr>`;
            })
            .join("")}</tbody>
        </table></div>`
      : emptyState("Мәлімет табылмады");
    els.adminContent.innerHTML = `
      <div class="card">
        <div class="admin-section-head">
          <div><p class="eyebrow">Төлемдер</p><h2>Төлемдер</h2></div>
        </div>
        ${paymentRows}
      </div>
      <div class="card">
        <div class="section-head"><h2>Қолмен тексеру</h2></div>
        <form id="paymentAction" class="form">
          <div class="grid two">
            <label class="field"><span>Төлем ID</span><input name="id" required placeholder="UUID" /></label>
            <label class="field"><span>Пікір / override</span><input name="comment" placeholder="Қабылдамау себебі немесе override түсіндірмесі" /></label>
          </div>
          <div class="action-row">
            <button class="gold-btn" name="action" value="approve" type="submit">Қабылдау</button>
            <button class="danger-btn" name="action" value="reject" type="submit">Қабылдамау</button>
          </div>
        </form>
      </div>
    `;
    document.getElementById("paymentAction").addEventListener("submit", async (event) => {
      event.preventDefault();
      const submitter = event.submitter;
      const form = new FormData(event.currentTarget);
      const id = form.get("id");
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
        renderAdminPayments();
      } catch (error) {
        toast(error.message || "Әрекет орындалмады", "error");
      }
    });
  }

  function receiptValidationSummary(receipt, payment) {
    const errors = receipt.validation_errors || [];
    return `<div class="receipt-validation">
      ${statusBadge(receipt.validation_status)}
      <span>Төлем сомасы: ${money(payment.amount_kzt)} ₸</span>
      <span>Чектегі сома: ${receipt.parsed_amount_kzt ? `${money(receipt.parsed_amount_kzt)} ₸` : "—"}</span>
      <span>Провайдер: ${esc(receipt.provider || "unknown")}</span>
      <span>QR: ${receipt.qr_found ? "QR табылды" : "QR табылмады"}</span>
      <span>Қайталану: ${receipt.duplicate_of_receipt_id ? "Күдікті/қайталанған" : "Бірегей"}</span>
      ${errors.length ? `<small>${esc(errors.join(", "))}</small>` : ""}
    </div>`;
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
