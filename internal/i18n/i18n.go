package i18n

var messages = map[string]map[string]string{
	"kk": {
		"choose_language":     "Тілді таңдаңыз:",
		"language_saved":      "Тіл сақталды. ZHENIS ORDA INSIDE жүйесіне қош келдіңіз.",
		"start":               "ZHENIS ORDA INSIDE жүйесіне қош келдіңіз.\nБұл жай курс емес. Бұл 12 айлық жүйелі өсу жолы.",
		"about":               "Сіз ойлау, қаржы, бизнес, проработка және лидерлік бойынша саты-саты өтіп, өзіңізді жаңа деңгейге шығарасыз.",
		"tariffs":             "BASIC — 4 990 KZT\nSTANDARD — 9 990 KZT\nVIP — 24 900 KZT",
		"diagnostics_done":    "Сізге алдымен “Мышление” фундаментінен бастау керек.",
		"payment_uploaded":    "Чек қабылданды. Әкімші тексерген соң сізге хабарлама келеді.",
		"payment_no_pending":  "Сізде тексерілуі керек pending төлем жоқ. Алдымен тариф таңдап, төлем жасаңыз.",
		"payment_approved":    "Қош келдіңіз!\nСіз ZHENIS ORDA INSIDE жүйесіне кірдіңіз.\nБірінші саты — МЫШЛЕНИЕ. Осы фундамент дұрыс қаланса, қаржы мен бизнес те ретке келеді.",
		"payment_rejected":    "Төлем чегі қабылданбады. Себебі: %s",
		"inactive_3_days":     "Ұстаз, сіз 3 күн сабаққа кірмедіңіз.\nЖолдан шықпайық. Бүгін 15 минут бөліп, келесі сабақты өтіңіз.",
		"subscription_ending": "Сіздің подпискаңыз 3 күннен кейін аяқталады.\nПрогрессіңіз сақталуы үшін төлемді ұзартыңыз.",
		"payment_stopped":     "Сіз LEVEL %d деңгейінде тоқтап қалдыңыз.\nАяқтауға әлі мүмкіндік бар. Қайта қосылыңыз.",
		"support_received":    "Қолдау қызметіне хабарламаңыз жіберілді.",
		"receipt_bad_file":    "Чек PDF, JPG, PNG немесе WEBP форматында болуы керек.",
		"receipt_too_large":   "Файл көлемі тым үлкен.",
		"open_mini_app":       "Mini App ашу",
		"menu_level":          "Менің деңгейім",
		"menu_lessons":        "Сабақтарым",
		"menu_test":           "Тест тапсыру",
		"menu_assignments":    "Тапсырмаларым",
		"menu_stream":         "Жабық эфир",
		"menu_referral":       "Реферал сілтемем",
		"menu_bonuses":        "Бонустарым",
		"menu_payment":        "Төлем мерзімі",
		"menu_support":        "Қолдау қызметі",
	},
	"ru": {
		"choose_language":     "Выберите язык:",
		"language_saved":      "Язык сохранен. Добро пожаловать в ZHENIS ORDA INSIDE.",
		"start":               "Добро пожаловать в ZHENIS ORDA INSIDE.\nЭто не просто курс. Это 12-месячный путь системного роста.",
		"about":               "Вы проходите мышление, финансы, бизнес, проработку и лидерство по уровням.",
		"tariffs":             "BASIC — 4 990 KZT\nSTANDARD — 9 990 KZT\nVIP — 24 900 KZT",
		"diagnostics_done":    "Вам нужно начать с фундамента “Мышление”.",
		"payment_uploaded":    "Чек принят. Администратор проверит его и отправит вам уведомление.",
		"payment_no_pending":  "У вас нет pending платежа. Сначала выберите тариф и оплатите.",
		"payment_approved":    "Добро пожаловать!\nВы вошли в систему ZHENIS ORDA INSIDE.\nПервая ступень — МЫШЛЕНИЕ.",
		"payment_rejected":    "Чек отклонен. Причина: %s",
		"inactive_3_days":     "Вы не заходили на уроки 3 дня.\nНе сходим с пути. Выделите сегодня 15 минут на следующий урок.",
		"subscription_ending": "Ваша подписка завершится через 3 дня.\nПродлите оплату, чтобы сохранить прогресс.",
		"payment_stopped":     "Вы остановились на LEVEL %d.\nЕще можно завершить путь. Возвращайтесь.",
		"support_received":    "Ваше сообщение отправлено в поддержку.",
		"receipt_bad_file":    "Чек должен быть в формате PDF, JPG, PNG или WEBP.",
		"receipt_too_large":   "Файл слишком большой.",
		"open_mini_app":       "Открыть Mini App",
		"menu_level":          "Мой уровень",
		"menu_lessons":        "Мои уроки",
		"menu_test":           "Пройти тест",
		"menu_assignments":    "Мои задания",
		"menu_stream":         "Закрытый эфир",
		"menu_referral":       "Реферальная ссылка",
		"menu_bonuses":        "Бонусы",
		"menu_payment":        "Срок оплаты",
		"menu_support":        "Поддержка",
	},
}

func T(language, key string) string {
	if language != "ru" {
		language = "kk"
	}
	if value, ok := messages[language][key]; ok {
		return value
	}
	if value, ok := messages["kk"][key]; ok {
		return value
	}
	return key
}
