package service

import "strings"

type PaymentProvider struct {
	Code  string `json:"code"`
	Title string `json:"title"`
}

func SupportedPaymentProviders() []PaymentProvider {
	return []PaymentProvider{
		{Code: "kaspi_qr", Title: "Kaspi QR"},
		{Code: "kaspi_pay", Title: "Kaspi Pay"},
		{Code: "halyk", Title: "Halyk"},
		{Code: "bank_card", Title: "Bank card"},
	}
}

func TariffRank(code string) int {
	switch strings.ToUpper(code) {
	case "VIP":
		return 3
	case "STANDARD":
		return 2
	case "BASIC":
		return 1
	default:
		return 0
	}
}

func ReferralRewardLabel(threshold int) string {
	switch threshold {
	case 1:
		return "7 days free"
	case 3:
		return "1 month free"
	case 5:
		return "closed VIP stream"
	case 10:
		return "personal mini-review"
	case 20:
		return "1 month VIP tariff access"
	case 50:
		return "personal Zoom with mentor"
	default:
		return ""
	}
}

func BrandTexts(language string) map[string]string {
	if language == "ru" {
		return map[string]string{
			"welcome": "Добро пожаловать в систему ZHENIS ORDA INSIDE.",
			"idea":    "Это не просто курс. Это 12-месячный путь системного роста.",
			"growth":  "Вы проходите мышление, финансы, бизнес, проработку и лидерство по уровням.",
			"first":   "Первая ступень — МЫШЛЕНИЕ.",
			"club":    "Жүйелі өсу ордасы.",
			"status":  "Статус. Гордость. Мотивация.",
		}
	}
	return map[string]string{
		"welcome": "ZHENIS ORDA INSIDE жүйесіне қош келдіңіз.",
		"idea":    "Бұл жай курс емес. Бұл 12 айлық жүйелі өсу жолы.",
		"growth":  "Сіз ойлау, қаржы, бизнес, проработка және лидерлік бойынша саты-саты өтіп, өзіңізді жаңа деңгейге шығарасыз.",
		"first":   "Бірінші саты — МЫШЛЕНИЕ.",
		"club":    "Жүйелі өсу ордасы.",
		"status":  "Статус. Мақтаныш. Мотивация.",
	}
}
