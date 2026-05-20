package repository_test

import (
	"errors"
	"testing"
	"time"

	"zhenis-orda-service/internal/repository"
)

func TestPremiumCourseAccessIndependentFromSubscription(t *testing.T) {
	store, ctx := newTestStore(t)
	user := registerUser(t, ctx, store, 12001, "")
	course, err := store.GetPremiumCourseBySlug(ctx, "altyn-formula")
	if err != nil {
		t.Fatal(err)
	}

	ok, err := store.HasPremiumCourseAccess(ctx, user.ID, course.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("premium access should not be active before grant or payment")
	}

	approveBasic(t, ctx, store, user.ID)
	ok, err = store.HasPremiumCourseAccess(ctx, user.ID, course.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("active subscription must not unlock premium course")
	}

	lessons, err := store.ListPremiumCourseLessons(ctx, user.ID, course.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(lessons) < 2 {
		t.Fatalf("expected seeded preview and locked lessons, got %d", len(lessons))
	}
	if !lessons[0].IsPreview || !lessons[0].Access {
		t.Fatalf("preview lesson should be open: %#v", lessons[0])
	}
	if lessons[1].Access || lessons[1].ContentText != "" {
		t.Fatalf("locked lesson should hide content: %#v", lessons[1])
	}
	if _, err := store.GetPremiumCourseLesson(ctx, user.ID, lessons[1].ID); !errors.Is(err, repository.ErrForbidden) {
		t.Fatalf("expected locked lesson forbidden, got %v", err)
	}

	if _, err := store.GrantPremiumCourseAccess(ctx, user.ID, course.ID, repository.PremiumAccessSourceManual, 1, nil, repository.PremiumAccessDurationLifetime, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE subscriptions SET expires_at = datetime('now', '-1 hour') WHERE user_id = ?`, user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ExpireSubscriptions(ctx); err != nil {
		t.Fatal(err)
	}
	sub, err := store.GetActiveSubscription(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sub != nil {
		t.Fatal("expected expired subscription")
	}
	ok, err = store.HasPremiumCourseAccess(ctx, user.ID, course.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("lifetime premium access should remain active after subscription expiry")
	}
	if _, err := store.GetPremiumCourseLesson(ctx, user.ID, lessons[1].ID); err != nil {
		t.Fatalf("premium lesson should open after manual grant: %v", err)
	}

	if err := store.RevokePremiumCourseAccess(ctx, user.ID, course.ID, 1); err != nil {
		t.Fatal(err)
	}
	ok, err = store.HasPremiumCourseAccess(ctx, user.ID, course.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("revoked premium access should close immediately")
	}
}

func TestPremiumCoursePaymentApprovalDoesNotCreateSubscription(t *testing.T) {
	store, ctx := newTestStore(t)
	user := registerUser(t, ctx, store, 12002, "")
	course, err := store.GetPremiumCourseBySlug(ctx, "biznes-praktikum")
	if err != nil {
		t.Fatal(err)
	}

	payment, err := store.CreatePremiumCoursePayment(ctx, user.ID, course.ID, repository.PaymentProviderKaspiQR, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if payment.PaymentType != repository.PaymentTypePremiumCourse {
		t.Fatalf("payment type = %s", payment.PaymentType)
	}
	if payment.AmountKZT != course.PriceKZT {
		t.Fatalf("premium payment amount = %d, want %d", payment.AmountKZT, course.PriceKZT)
	}

	approved, err := store.ApprovePayment(ctx, payment.ID, 1, 30)
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != repository.PaymentStatusApproved {
		t.Fatalf("payment status = %s", approved.Status)
	}
	if approved.SubscriptionID != nil {
		t.Fatalf("premium payment should not link a subscription: %#v", approved.SubscriptionID)
	}
	sub, err := store.GetActiveSubscription(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sub != nil {
		t.Fatalf("premium payment should not create subscription: %#v", sub)
	}
	updated, err := store.GetUserByID(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.CurrentLevel != 0 {
		t.Fatalf("premium payment should not change normal level, got %d", updated.CurrentLevel)
	}
	ok, err := store.HasPremiumCourseAccess(ctx, user.ID, course.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("approved premium payment should grant premium course access")
	}
}
