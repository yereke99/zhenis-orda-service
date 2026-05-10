(function () {
  const tg = window.Telegram && window.Telegram.WebApp ? window.Telegram.WebApp : null;
  const isAdmin = window.location.pathname === "/admin";
  const state = {
    me: null,
    platform: null,
    levels: [],
    lessons: [],
    currentScreen: "dashboard",
    selectedTariff: null,
    adminScreen: "dashboard",
    admin: null,
  };

  setupTelegramViewport();

  document.addEventListener("DOMContentLoaded", () => {
    if (isAdmin) {
      document.body.classList.add("admin-body");
      document.getElementById("miniApp").classList.add("hidden");
      document.getElementById("adminApp").classList.remove("hidden");
      initAdmin();
      return;
    }
    document.body.classList.add("mini-body");
    renderUserHeader();
    initMiniApp();
  });

  function setupTelegramViewport() {
    updateViewportVars();
    if (tg) {
      try {
        tg.ready();
      } catch (_) {}
      forceExpand();
      ["viewportChanged", "safeAreaChanged", "contentSafeAreaChanged", "fullscreenChanged", "fullscreenFailed"].forEach((eventName) => {
        try {
          tg.onEvent(eventName, syncTelegramFrame);
        } catch (_) {}
      });
    }
    ["resize", "orientationchange", "focus", "visibilitychange"].forEach((eventName) => {
      window.addEventListener(eventName, syncTelegramFrame, { passive: true });
    });
  }

  function forceExpand() {
    [0, 120, 450, 900].forEach((delay) => {
      setTimeout(() => {
        if (!tg) return;
        try {
          tg.ready();
        } catch (_) {}
        try {
          tg.expand();
        } catch (_) {}
        try {
          if (typeof tg.requestFullscreen === "function") tg.requestFullscreen();
        } catch (_) {}
        try {
          if (typeof tg.disableVerticalSwipes === "function") tg.disableVerticalSwipes();
        } catch (_) {}
        syncTelegramFrame();
      }, delay);
    });
  }

  function syncTelegramFrame() {
    updateViewportVars();
    if (tg) {
      try {
        tg.expand();
      } catch (_) {}
    }
  }

  function updateViewportVars() {
    const viewportHeight = tg && tg.viewportHeight ? tg.viewportHeight : window.innerHeight;
    const stableHeight = tg && tg.viewportStableHeight ? tg.viewportStableHeight : viewportHeight;
    const safe = (tg && tg.safeAreaInset) || {};
    const contentSafe = (tg && tg.contentSafeAreaInset) || {};
    const top = Math.max(14, Number(safe.top || 0) + Number(contentSafe.top || 0) + 14);
    const bottom = Math.max(12, Number(safe.bottom || 0) + Number(contentSafe.bottom || 0) + 10);
    const root = document.documentElement;
    root.style.setProperty("--viewport-height", `${Math.round(viewportHeight)}px`);
    root.style.setProperty("--viewport-stable-height", `${Math.round(stableHeight)}px`);
    root.style.setProperty("--tg-safe-area-inset-top", `${Number(safe.top || 0)}px`);
    root.style.setProperty("--tg-safe-area-inset-bottom", `${Number(safe.bottom || 0)}px`);
    root.style.setProperty("--tg-content-safe-area-inset-top", `${Number(contentSafe.top || 0)}px`);
    root.style.setProperty("--tg-content-safe-area-inset-bottom", `${Number(contentSafe.bottom || 0)}px`);
    root.style.setProperty("--app-top-padding", `${top}px`);
    root.style.setProperty("--app-bottom-padding", `${bottom}px`);
  }

  function renderUserHeader() {
    const tgUser = (tg && tg.initDataUnsafe && tg.initDataUnsafe.user) || {};
    const name = [tgUser.first_name, tgUser.last_name].filter(Boolean).join(" ") || "Senior coffee Drinker";
    const login = tgUser.username ? `@${tgUser.username}` : tgUser.id ? `ID ${tgUser.id}` : "@zhenis_orda_inside";
    const avatar = document.getElementById("tgAvatar");
    const fallback = document.getElementById("tgAvatarFallback");
    document.getElementById("tgName").textContent = name;
    document.getElementById("tgLogin").textContent = login;
    fallback.textContent = name.trim().charAt(0).toUpperCase() || "Z";
    if (tgUser.photo_url) {
      avatar.src = tgUser.photo_url;
      avatar.onload = () => {
        avatar.style.display = "block";
        fallback.style.display = "none";
      };
      avatar.onerror = () => {
        avatar.style.display = "none";
        fallback.style.display = "grid";
      };
    }
  }

  async function initMiniApp() {
    renderShellLoading();
    try {
      const [me, platform, levels] = await Promise.all([api("/api/me"), api("/api/platform"), api("/api/levels")]);
      state.me = me;
      state.platform = platform;
      state.levels = levels.levels || [];
      state.currentScreen = me.user && me.user.current_level > 0 ? "dashboard" : "onboarding";
      renderMini();
    } catch (error) {
      renderError(error.message || "Mini App auth failed");
    }
  }

  async function api(path, options = {}) {
    const headers = Object.assign({ "Content-Type": "application/json" }, options.headers || {});
    if (tg && tg.initData) headers["X-Telegram-Init-Data"] = tg.initData;
    if (!tg || !tg.initData) headers["X-Miniapp-Dev"] = "1";
    const response = await fetch(path, Object.assign({}, options, { headers, credentials: "include" }));
    const data = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(data.error || `HTTP ${response.status}`);
    return data;
  }

  function renderMini() {
    renderFooter();
    const screen = state.currentScreen;
    if (screen === "onboarding") return renderOnboarding();
    if (screen === "diagnostics") return renderDiagnostics();
    if (screen === "tariffs") return renderTariffs();
    if (screen === "payment") return renderPayment();
    if (screen === "dashboard") return renderDashboard();
    if (screen === "levels") return renderLevels();
    if (screen === "lessons") return renderLessons();
    if (screen === "test") return renderTest();
    if (screen === "assignment") return renderAssignment();
    if (screen === "referral") return renderReferral();
    if (screen === "coins") return renderCoins();
    if (screen === "streams") return renderStreams();
    if (screen === "channels") return renderChannels();
    if (screen === "profile") return renderProfile();
    if (screen === "support") return renderSupport();
  }

  function setScreen(screen) {
    state.currentScreen = screen;
    renderMini();
    document.getElementById("appContent").scrollTo({ top: 0, behavior: "smooth" });
  }

  function renderFooter() {
    const tabs = [
      ["dashboard", "D", "Басты"],
      ["lessons", "L", "Сабақ"],
      ["tariffs", "P", "Төлем"],
      ["referral", "R", "Реф"],
      ["profile", "M", "Профиль"],
    ];
    document.getElementById("bottomCta").innerHTML = `<div class="tabbar">${tabs
      .map(([screen, icon, label]) => `<button class="tab-btn ${state.currentScreen === screen ? "active" : ""}" data-screen="${screen}" type="button"><span>${icon}</span><span>${label}</span></button>`)
      .join("")}</div>`;
    document.querySelectorAll("[data-screen]").forEach((button) => button.addEventListener("click", () => setScreen(button.dataset.screen)));
  }

  function renderOnboarding() {
    html(`
      <section class="screen">
        <div class="hero">
          <p class="eyebrow">ZHENIS ORDA INSIDE</p>
          <h1>Бұл жай курс емес. Бұл 12 айлық жүйелі өсу жолы.</h1>
          <p class="muted">Сіз ойлау, қаржы, бизнес, проработка және лидерлік бойынша саты-саты өтіп, өзіңізді жаңа деңгейге шығарасыз.</p>
          <div class="pill-row"><span class="pill">Статус</span><span class="pill">Мақтаныш</span><span class="pill">Мотивация</span></div>
        </div>
        <div class="grid two">
          <button class="gold-btn" id="goDiagnostics" type="button">Тегін диагностика</button>
          <button class="ghost-btn" id="goTariffs" type="button">Тариф таңдау</button>
        </div>
      </section>
    `);
    on("goDiagnostics", () => setScreen("diagnostics"));
    on("goTariffs", () => setScreen("tariffs"));
  }

  function renderDashboard() {
    const user = state.me.user;
    const progress = state.me.progress || {};
    const sub = user.subscription || {};
    html(`
      <section class="screen">
        <div class="hero">
          <p class="eyebrow">Жүйелі өсу ордасы</p>
          <h1>ZHENIS ORDA INSIDE жүйесіне қош келдіңіз.</h1>
          <p class="muted">Бірінші саты — МЫШЛЕНИЕ. Осы фундамент дұрыс қаланса, қаржы мен бизнес те ретке келеді.</p>
          <div class="progress-track"><div class="progress-fill" style="--progress:${num(progress.percent)}%"></div></div>
          <strong>Сіздің прогрессіңіз: ${num(progress.percent)}%</strong>
        </div>
        <div class="grid three">
          ${metric("Тариф", sub.tariff_code || "Жоқ", sub.status || "inactive")}
          ${metric("Деңгей", `LEVEL ${user.current_level || 0}`, progress.next_requirement || "Төлем жасаңыз")}
          ${metric("ZHENIS COIN", `${num(user.coin_balance)}`, "Ішкі валюта")}
        </div>
        <div class="card">
          <div class="card-header">
            <div><p class="eyebrow">Unlock</p><h2>Келесі талап</h2></div>
            <span class="status ${progress.can_unlock_next ? "ok" : ""}">${progress.can_unlock_next ? "Ready" : "Locked"}</span>
          </div>
          <p class="muted">${esc(progress.next_requirement || "LEVEL 2 ашылуы үшін тест тапсырыңыз.")}</p>
        </div>
        ${renderLevelStrip()}
        <div class="grid two">
          <button class="ghost-btn" data-next="lessons" type="button">Сабақтарым</button>
          <button class="ghost-btn" data-next="test" type="button">Тест тапсыру</button>
          <button class="ghost-btn" data-next="streams" type="button">Жабық эфир</button>
          <button class="ghost-btn" data-next="channels" type="button">Жабық каналдар</button>
          <button class="ghost-btn" data-next="coins" type="button">Бонустарым / Coin</button>
          <button class="ghost-btn" data-next="support" type="button">Қолдау қызметі</button>
        </div>
      </section>
    `);
    document.querySelectorAll("[data-next]").forEach((button) => button.addEventListener("click", () => setScreen(button.dataset.next)));
  }

  function renderLevelStrip() {
    return `<div class="level-strip">${state.levels
      .map((level) => `<button class="level-token ${level.access ? "open" : ""}" data-level="${level.number}" type="button">LEVEL ${level.number}<br>${esc(level.title_kk)}</button>`)
      .join("")}</div>`;
  }

  async function renderLevels() {
    const levels = state.levels;
    html(`<section class="screen"><h1>Менің деңгейім</h1>${renderLevelStrip()}<div class="grid">${levels.map(levelCard).join("")}</div></section>`);
  }

  function levelCard(level) {
    const progress = level.progress || {};
    return `<article class="card">
      <div class="card-header"><div><p class="eyebrow">LEVEL ${level.number}</p><h2>${esc(level.title_kk)}</h2></div><span class="status ${level.access ? "ok" : "bad"}">${level.access ? "Open" : "Locked"}</span></div>
      <div class="progress-track"><div class="progress-fill" style="--progress:${num(progress.percent)}%"></div></div>
      <p class="muted">${esc(progress.next_requirement || "")}</p>
    </article>`;
  }

  async function renderLessons() {
    const level = state.me.user.current_level || 1;
    const data = await api(`/api/lessons?level=${level}`);
    state.lessons = data.lessons || [];
    html(`
      <section class="screen">
        <div class="card-header"><div><p class="eyebrow">LEVEL ${level}</p><h1>Сабақтарым</h1></div><button class="ghost-btn" id="refreshLessons" type="button">Жаңарту</button></div>
        <div class="grid">${state.lessons.map(lessonCard).join("") || empty("Сабақ жоқ")}</div>
      </section>
    `);
    on("refreshLessons", renderLessons);
    document.querySelectorAll("[data-watch]").forEach((button) => button.addEventListener("click", () => markWatched(button.dataset.watch)));
  }

  function lessonCard(lesson) {
    return `<article class="lesson-card ${lesson.access ? "" : "locked"}">
      <div class="card-header">
        <div><p class="eyebrow">Сабақ ${lesson.sort_order}</p><h2>${esc(lesson.title_kk)}</h2></div>
        <span class="status ${lesson.watched ? "ok" : ""}">${lesson.watched ? "Watched" : lesson.access ? "Open" : "Locked"}</span>
      </div>
      <p class="muted">${esc(lesson.description_kk || "ZHENIS ORDA INSIDE")}</p>
      <button class="${lesson.watched ? "ghost-btn" : "gold-btn"}" data-watch="${lesson.id}" ${lesson.access ? "" : "disabled"} type="button">${lesson.watched ? "Қайта белгілеу" : "Сабақты өттім"}</button>
    </article>`;
  }

  async function markWatched(id) {
    await api(`/api/lessons/${id}/watched`, { method: "POST", body: "{}" });
    state.me = await api("/api/me");
    await refreshLevels();
    setScreen("lessons");
  }

  async function renderTest() {
    const level = state.me.user.current_level || 1;
    let data;
    try {
      data = await api(`/api/tests/${level}`);
    } catch (error) {
      return html(`<section class="screen"><h1>Тест</h1>${empty(error.message)}</section>`);
    }
    const test = data.test;
    html(`
      <section class="screen">
        <div class="card"><p class="eyebrow">LEVEL ${level}</p><h1>${esc(test.title)}</h1><p class="muted">Өту шегі: ${test.pass_percent}%</p></div>
        <form id="testForm" class="form">${test.questions.map(questionBlock).join("")}<button class="gold-btn" type="submit">Тест тапсыру</button></form>
      </section>
    `);
    document.getElementById("testForm").addEventListener("submit", async (event) => {
      event.preventDefault();
      const answers = {};
      new FormData(event.currentTarget).forEach((value, key) => (answers[key] = Number(value)));
      const result = await api(`/api/tests/${level}/submit`, { method: "POST", body: JSON.stringify({ answers }) });
      state.me = await api("/api/me");
      await refreshLevels();
      alert(`Score: ${result.attempt.score_percent}%`);
      setScreen("dashboard");
    });
  }

  function questionBlock(question) {
    return `<fieldset class="card"><h3>${esc(question.question_text_kk)}</h3>${question.options
      .map((option) => `<label class="test-option"><input name="${question.id}" value="${option.id}" type="radio" required /> <span>${esc(option.option_text_kk)}</span></label>`)
      .join("")}</fieldset>`;
  }

  async function renderAssignment() {
    const level = state.me.user.current_level || 1;
    let assignment;
    try {
      assignment = (await api(`/api/assignments/${level}`)).assignment;
    } catch (error) {
      return html(`<section class="screen"><h1>Тапсырмаларым</h1>${empty("Бұл деңгейде тапсырма жоқ немесе әлі ашылмаған.")}</section>`);
    }
    html(`
      <section class="screen">
        <div class="card"><p class="eyebrow">LEVEL ${level}</p><h1>${esc(assignment.title_kk)}</h1><p class="muted">${esc(assignment.description_kk || "")}</p></div>
        <form id="assignmentForm" class="form">
          <label class="field"><span>Жауап</span><textarea name="answer_text" required></textarea></label>
          <label class="field"><span>Сілтеме</span><input name="link_url" placeholder="https://" /></label>
          <button class="gold-btn" type="submit">Жіберу</button>
        </form>
      </section>
    `);
    document.getElementById("assignmentForm").addEventListener("submit", async (event) => {
      event.preventDefault();
      const body = Object.fromEntries(new FormData(event.currentTarget).entries());
      await api(`/api/assignments/${level}/submit`, { method: "POST", body: JSON.stringify(body) });
      state.me = await api("/api/me");
      setScreen("dashboard");
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
    html(`<section class="screen"><div class="card"><p class="eyebrow">Free diagnostics</p><h1>Диагностика</h1><p class="muted">Нәтижеден кейін жүйе сізге бірінші фундаментті ұсынады.</p></div><form id="diagnosticsForm" class="form">${fields
      .map(([key, label]) => `<label class="field"><span>${label}</span><input name="${key}" required /></label>`)
      .join("")}<button class="gold-btn" type="submit">Жіберу</button></form></section>`);
    document.getElementById("diagnosticsForm").addEventListener("submit", async (event) => {
      event.preventDefault();
      const answers = Object.fromEntries(new FormData(event.currentTarget).entries());
      const res = await api("/api/diagnostics", { method: "POST", body: JSON.stringify({ answers }) });
      alert(res.message);
      setScreen("tariffs");
    });
  }

  async function renderTariffs() {
    if (!state.platform) state.platform = await api("/api/platform");
    html(`<section class="screen"><div class="card"><p class="eyebrow">Membership</p><h1>Тарифтер</h1><p class="muted">LEVEL 1 төлемнен кейін ашылады. Контент саты-саты ашылады.</p></div><div class="grid three">${(state.platform.tariffs || [])
      .map(tariffCard)
      .join("")}</div></section>`);
    document.querySelectorAll("[data-tariff]").forEach((button) =>
      button.addEventListener("click", () => {
        state.selectedTariff = button.dataset.tariff;
        setScreen("payment");
      }),
    );
  }

  function tariffCard(tariff) {
    return `<article class="tariff-card ${tariff.code === "STANDARD" ? "featured" : ""}">
      <header><div><p class="eyebrow">${esc(tariff.code)}</p><h2>${money(tariff.price_kzt)} KZT / month</h2></div><span class="pill">${tariff.code}</span></header>
      <ul>${(tariff.features || []).map((item) => `<li>${esc(item)}</li>`).join("")}</ul>
      <button class="gold-btn" data-tariff="${tariff.code}" type="button">Таңдау</button>
    </article>`;
  }

  function renderPayment() {
    const tariff = state.selectedTariff || "BASIC";
    html(`
      <section class="screen">
        <div class="card"><p class="eyebrow">Manual MVP payment</p><h1>${tariff}</h1><p class="muted">Kaspi QR / Kaspi Pay арқылы төлем жасап, чекті Telegram ботқа PDF/image ретінде жіберіңіз.</p></div>
        <form id="paymentForm" class="form">
          <label class="field"><span>Payment provider</span><select name="provider"><option value="kaspi_qr">Kaspi QR</option><option value="kaspi_pay">Kaspi Pay</option><option value="halyk">Halyk</option><option value="bank_card">Bank card</option></select></label>
          <button class="gold-btn" type="submit">Pending төлем құру</button>
        </form>
        <div id="paymentResult"></div>
      </section>
    `);
    document.getElementById("paymentForm").addEventListener("submit", async (event) => {
      event.preventDefault();
      const provider = new FormData(event.currentTarget).get("provider");
      const res = await api("/api/payments", { method: "POST", body: JSON.stringify({ tariff_code: tariff, provider }) });
      document.getElementById("paymentResult").innerHTML = `<div class="card"><h2>Payment #${res.payment.id}</h2><p class="muted">${esc(res.instructions.text)}</p><p>Amount: <strong>${money(res.payment.amount_kzt)} KZT</strong></p></div>`;
    });
  }

  async function renderReferral() {
    const data = await api("/api/referral");
    const referral = data.referral;
    html(`<section class="screen"><div class="hero"><p class="eyebrow">Referral</p><h1>Жаңа клиент шақырыңыз</h1><p class="muted">Нақты тіркелген және төлемі мақұлданған клиенттер ғана reward береді.</p></div><div class="card"><h2>Сілтеме</h2><p class="muted">${esc(referral.referral_link)}</p><button class="gold-btn" id="copyRef" type="button">Көшіру</button></div><div class="grid two">${metric("Шақырылған", referral.invited_count, "registered")}${metric("Төлем жасаған", referral.paid_count, "approved")}</div></section>`);
    on("copyRef", () => navigator.clipboard && navigator.clipboard.writeText(referral.referral_link));
  }

  async function renderCoins() {
    const [coins, bonuses] = await Promise.all([api("/api/coins"), api("/api/bonuses")]);
    html(`<section class="screen"><div class="hero"><p class="eyebrow">ZHENIS COIN</p><h1>${num(coins.balance)} coin</h1><p class="muted">Lesson watched +5, test passed +20, stream attended +30, referral +100.</p></div><div class="grid">${bonuses.plan
      .map((item) => `<div class="card"><strong>${item.count} referral</strong><p class="muted">${esc(item.reward)}</p></div>`)
      .join("")}</div></section>`);
  }

  async function renderStreams() {
    const data = await api("/api/streams");
    html(`<section class="screen"><div class="card"><p class="eyebrow">ZHABYQ RAZBOR NIGHT</p><h1>Жабық эфир</h1><p class="muted">STANDARD және VIP үшін жазбалар эфирден кейін ашылады.</p></div><div class="grid">${(data.streams || [])
      .map((stream) => `<div class="card"><h2>${esc(stream.title)}</h2><p class="muted">${new Date(stream.starts_at).toLocaleString()}</p><span class="pill">${esc(stream.tariff_requirement)}</span></div>`)
      .join("") || empty("Эфир әлі жоспарланбаған")}</div></section>`);
  }

  async function renderChannels() {
    const data = await api("/api/channels");
    html(`<section class="screen"><div class="card"><p class="eyebrow">Private access</p><h1>Жабық каналдар</h1></div><div class="grid">${(data.channels || [])
      .map((channel) => `<div class="card"><div class="card-header"><div><h2>${esc(channel.title)}</h2><p class="muted">${esc(channel.tariff_requirement)} · LEVEL ${channel.level_requirement}</p></div><span class="status ${channel.access ? "ok" : "bad"}">${channel.access ? "Open" : "Locked"}</span></div><button class="gold-btn" data-invite="${channel.id}" ${channel.access ? "" : "disabled"} type="button">Invite link алу</button></div>`)
      .join("") || empty("Каналдар жоқ")}</div></section>`);
    document.querySelectorAll("[data-invite]").forEach((button) =>
      button.addEventListener("click", async () => {
        const res = await api(`/api/channels/${button.dataset.invite}/invite`, { method: "POST", body: "{}" });
        alert(res.invite_link);
      }),
    );
  }

  function renderProfile() {
    const user = state.me.user;
    const sub = user.subscription || {};
    html(`<section class="screen"><div class="card"><p class="eyebrow">Profile</p><h1>${esc([user.first_name, user.last_name].filter(Boolean).join(" ") || user.username || "User")}</h1><p class="muted">@${esc(user.username || String(user.telegram_id))}</p></div><div class="grid two">${metric("Current tariff", sub.tariff_code || "None", sub.status || "inactive")}${metric("Payment expiration", sub.expires_at ? new Date(sub.expires_at).toLocaleDateString() : "No active subscription", "subscription")}${metric("Current level", `LEVEL ${user.current_level || 0}`, "12-month journey")}${metric("Coin balance", num(user.coin_balance), "ZHENIS COIN")}</div><button class="ghost-btn" data-next="support" type="button">Қолдау қызметі</button></section>`);
    document.querySelectorAll("[data-next]").forEach((button) => button.addEventListener("click", () => setScreen(button.dataset.next)));
  }

  function renderSupport() {
    html(`<section class="screen"><div class="card"><p class="eyebrow">Support</p><h1>Қолдау қызметі</h1></div><form id="supportForm" class="form"><label class="field"><span>Хабарлама</span><textarea name="body" required></textarea></label><button class="gold-btn" type="submit">Жіберу</button></form></section>`);
    document.getElementById("supportForm").addEventListener("submit", async (event) => {
      event.preventDefault();
      const body = new FormData(event.currentTarget).get("body");
      const res = await api("/api/support", { method: "POST", body: JSON.stringify({ body }) });
      alert(res.message);
      setScreen("dashboard");
    });
  }

  async function refreshLevels() {
    const levels = await api("/api/levels");
    state.levels = levels.levels || [];
  }

  function renderShellLoading() {
    html(`<section class="screen"><div class="hero"><p class="eyebrow">ZHENIS ORDA INSIDE</p><h1>Жүктелуде...</h1></div></section>`);
  }

  function renderError(message) {
    html(`<section class="screen"><div class="card"><h1>Қате</h1><p class="muted">${esc(message)}</p></div></section>`);
  }

  function html(markup) {
    document.getElementById("appContent").innerHTML = markup;
  }

  function metric(label, value, hint) {
    return `<div class="metric"><p class="eyebrow">${esc(label)}</p><strong>${esc(String(value))}</strong><span class="muted">${esc(String(hint || ""))}</span></div>`;
  }

  function empty(text) {
    return `<div class="card"><p class="muted">${esc(text)}</p></div>`;
  }

  function on(id, fn) {
    const el = document.getElementById(id);
    if (el) el.addEventListener("click", fn);
  }

  function esc(value) {
    return String(value || "").replace(/[&<>"']/g, (ch) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[ch]);
  }

  function num(value) {
    return Number(value || 0);
  }

  function money(value) {
    return num(value).toLocaleString("ru-RU");
  }

  async function initAdmin() {
    renderAdminNav();
    document.getElementById("adminLogout").addEventListener("click", async () => {
      await api("/api/browser-auth/logout", { method: "POST", body: "{}" }).catch(() => {});
      state.admin = null;
      renderAdminLogin();
    });
    try {
      const me = await api("/api/browser-auth/me");
      state.admin = me.admin;
      renderAdmin();
    } catch (_) {
      renderAdminLogin();
    }
  }

  function renderAdminLogin() {
    document.getElementById("adminNav").innerHTML = "";
    document.getElementById("adminTitle").textContent = "Admin login";
    document.getElementById("adminContent").innerHTML = `<div class="login-panel card"><p class="eyebrow">Secure browser session</p><h1>ZHENIS ORDA Admin</h1><form id="adminLoginForm" class="form"><label class="field"><span>Password</span><input name="password" type="password" autocomplete="current-password" required /></label><button class="gold-btn" type="submit">Login</button></form><p class="muted">Development default: admin</p></div>`;
    document.getElementById("adminLoginForm").addEventListener("submit", async (event) => {
      event.preventDefault();
      const password = new FormData(event.currentTarget).get("password");
      const res = await api("/api/browser-auth/login", { method: "POST", body: JSON.stringify({ password }) });
      state.admin = res.admin;
      renderAdminNav();
      renderAdmin();
    });
  }

  const adminScreens = [
    ["dashboard", "Dashboard"],
    ["users", "Users"],
    ["payments", "Payments"],
    ["subscriptions", "Subscriptions"],
    ["levels", "Levels"],
    ["lessons", "Lessons"],
    ["tests", "Tests"],
    ["assignments", "Assignments"],
    ["referrals", "Referrals"],
    ["coins", "Coins"],
    ["channels", "Channels"],
    ["streams", "Streams"],
    ["broadcast", "Broadcast"],
    ["analytics", "Analytics"],
    ["settings", "Settings"],
    ["audit", "Audit log"],
  ];

  function renderAdminNav() {
    const nav = document.getElementById("adminNav");
    if (!nav) return;
    nav.innerHTML = adminScreens.map(([key, label]) => `<button class="tool-btn ${state.adminScreen === key ? "active" : ""}" data-admin-screen="${key}" type="button">${label}</button>`).join("");
    document.querySelectorAll("[data-admin-screen]").forEach((button) =>
      button.addEventListener("click", () => {
        state.adminScreen = button.dataset.adminScreen;
        renderAdminNav();
        renderAdmin();
      }),
    );
  }

  async function renderAdmin() {
    document.getElementById("adminTitle").textContent = adminScreens.find(([key]) => key === state.adminScreen)?.[1] || "Dashboard";
    if (state.adminScreen === "dashboard" || state.adminScreen === "analytics") return renderAdminDashboard();
    if (state.adminScreen === "users") return renderAdminList("/api/admin/users", "users", ["id", "telegram_id", "username", "current_level", "coin_balance"]);
    if (state.adminScreen === "payments") return renderAdminPayments();
    if (state.adminScreen === "subscriptions") return renderAdminList("/api/admin/subscriptions", "subscriptions", ["id", "user_id", "tariff_code", "status", "expires_at"]);
    if (state.adminScreen === "levels") return renderAdminList("/api/admin/levels", "levels", ["number", "title_kk", "access"]);
    if (state.adminScreen === "lessons") return renderAdminList("/api/admin/lessons", "lessons", ["id", "level_number", "title_kk", "watched"]);
    if (state.adminScreen === "tests") return renderAdminItems("/api/admin/tests");
    if (state.adminScreen === "assignments") return renderAdminItems("/api/admin/assignments/submissions");
    if (state.adminScreen === "referrals") return renderAdminItems("/api/admin/referrals");
    if (state.adminScreen === "coins") return renderAdminItems("/api/admin/coins");
    if (state.adminScreen === "channels") return renderAdminChannels();
    if (state.adminScreen === "streams") return renderAdminList("/api/admin/streams", "streams", ["id", "title", "starts_at", "tariff_requirement", "status"]);
    if (state.adminScreen === "broadcast") return renderAdminBroadcast();
    if (state.adminScreen === "settings") return renderAdminSettings();
    if (state.adminScreen === "audit") return renderAdminList("/api/admin/audit", "actions", ["id", "role", "action", "entity_type", "created_at"]);
  }

  async function renderAdminDashboard() {
    const data = await api("/api/admin/stats");
    const stats = data.stats;
    document.getElementById("adminContent").innerHTML = `<div class="admin-grid">${Object.entries(stats).map(([key, value]) => metric(key.replaceAll("_", " "), value, "live")).join("")}</div>`;
  }

  async function renderAdminList(url, key, columns) {
    const data = await api(url);
    const rows = data[key] || data.items || [];
    document.getElementById("adminContent").innerHTML = table(columns, rows);
  }

  async function renderAdminItems(url) {
    const data = await api(url);
    const rows = data.items || [];
    const columns = rows[0] ? Object.keys(rows[0]).slice(0, 7) : ["id", "status"];
    document.getElementById("adminContent").innerHTML = table(columns, rows);
  }

  async function renderAdminPayments() {
    const data = await api("/api/admin/payments");
    const rows = data.payments || [];
    document.getElementById("adminContent").innerHTML = `${table(["id", "user_id", "tariff_code", "amount_kzt", "provider", "status", "receipt_file_path"], rows)}<div class="card"><h2>Manual verification</h2><form id="paymentAction" class="form"><label class="field"><span>Payment ID</span><input name="id" required /></label><label class="field"><span>Comment</span><input name="comment" /></label><div class="action-row"><button class="gold-btn" name="action" value="approve" type="submit">Approve</button><button class="danger-btn" name="action" value="reject" type="submit">Reject</button></div></form></div>`;
    document.getElementById("paymentAction").addEventListener("submit", async (event) => {
      event.preventDefault();
      const submitter = event.submitter;
      const form = new FormData(event.currentTarget);
      const id = form.get("id");
      if (submitter.value === "approve") await api(`/api/admin/payments/${id}/approve`, { method: "POST", body: JSON.stringify({ days: 30 }) });
      else await api(`/api/admin/payments/${id}/reject`, { method: "POST", body: JSON.stringify({ comment: form.get("comment") }) });
      renderAdminPayments();
    });
  }

  async function renderAdminChannels() {
    const data = await api("/api/admin/channels");
    document.getElementById("adminContent").innerHTML = `${table(["id", "title", "telegram_chat_id", "tariff_requirement", "level_requirement", "is_active"], data.channels || [])}<div class="card"><h2>Add channel</h2><form id="channelForm" class="form"><label class="field"><span>Title</span><input name="title" required /></label><label class="field"><span>Telegram chat ID</span><input name="telegram_chat_id" required /></label><label class="field"><span>Manual invite link</span><input name="manual_invite_link" /></label><label class="field"><span>Tariff</span><select name="tariff_requirement"><option>BASIC</option><option>STANDARD</option><option>VIP</option></select></label><label class="field"><span>Level</span><input name="level_requirement" type="number" value="1" min="1" max="12" /></label><button class="gold-btn" type="submit">Save</button></form></div>`;
    document.getElementById("channelForm").addEventListener("submit", async (event) => {
      event.preventDefault();
      const body = Object.fromEntries(new FormData(event.currentTarget).entries());
      body.level_requirement = Number(body.level_requirement || 1);
      body.invite_link_type = body.manual_invite_link ? "manual" : "bot";
      body.is_active = true;
      await api("/api/admin/channels", { method: "POST", body: JSON.stringify(body) });
      renderAdminChannels();
    });
  }

  function renderAdminBroadcast() {
    document.getElementById("adminContent").innerHTML = `<div class="card"><h2>Broadcast</h2><form id="broadcastForm" class="form"><label class="field"><span>Title</span><input name="title" /></label><label class="field"><span>Message</span><textarea name="body" required></textarea></label><button class="gold-btn" type="submit">Queue broadcast</button></form></div>`;
    document.getElementById("broadcastForm").addEventListener("submit", async (event) => {
      event.preventDefault();
      const body = Object.fromEntries(new FormData(event.currentTarget).entries());
      await api("/api/admin/broadcast", { method: "POST", body: JSON.stringify(body) });
      alert("Broadcast queued");
    });
  }

  async function renderAdminSettings() {
    const data = await api("/api/admin/settings");
    document.getElementById("adminContent").innerHTML = `<div class="card"><h2>Settings</h2><pre>${esc(JSON.stringify(data.settings, null, 2))}</pre></div>`;
  }

  function table(columns, rows) {
    if (!rows.length) return empty("No data yet");
    return `<div class="table-wrap"><table><thead><tr>${columns.map((col) => `<th>${esc(col)}</th>`).join("")}</tr></thead><tbody>${rows
      .map((row) => `<tr>${columns.map((col) => `<td>${esc(formatCell(row[col]))}</td>`).join("")}</tr>`)
      .join("")}</tbody></table></div>`;
  }

  function formatCell(value) {
    if (value === null || value === undefined) return "";
    if (typeof value === "object") return JSON.stringify(value);
    return String(value);
  }
})();
