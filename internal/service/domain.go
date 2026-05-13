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
		{Code: "bank_card", Title: "Банк картасы"},
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
		return "7 күн тегін"
	case 3:
		return "1 ай тегін"
	case 5:
		return "жабық VIP эфир"
	case 10:
		return "жеке мини-талдау"
	case 20:
		return "VIP тарифіне 1 ай қолжетімділік"
	case 50:
		return "ментормен жеке Zoom"
	default:
		return ""
	}
}

func BrandTexts(language string) map[string]string {
	return map[string]string{
		"welcome": "ZHENIS ORDA INSIDE жүйесіне қош келдіңіз.",
		"idea":    "Бұл жай курс емес. Бұл 12 айлық жүйелі өсу жолы.",
		"growth":  "Сіз ойлау, қаржы, бизнес, ішкі жұмыс және көшбасшылық бойынша саты-саты өтіп, өзіңізді жаңа деңгейге шығарасыз.",
		"first":   "Бірінші саты — ОЙЛАУ.",
		"club":    "Жүйелі өсу ордасы.",
		"status":  "Мәртебе. Мақтаныш. Мотивация.",
	}
}
