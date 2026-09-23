package handlers

import (
	"database/sql"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	middleware "backend/middlewares"
	"backend/utils"

	"github.com/redis/go-redis/v9"
	"github.com/stripe/stripe-go/v85"
	"github.com/stripe/stripe-go/v85/checkout/session"
	"github.com/stripe/stripe-go/v85/subscription"
	"github.com/stripe/stripe-go/v85/webhook"
)

type Stripe struct {
	Db          *sql.DB
	Redis       *redis.Client
	FrontendURL string
}

func (s *Stripe) CreateCheckoutSession(w http.ResponseWriter, r *http.Request) {
	priceID := strings.TrimSpace(os.Getenv("STRIPE_PRICE_ID"))
	if priceID == "" {
		utils.RespondError(w, http.StatusInternalServerError, "Billing is not configured")
		return
	}

	userID, ok := r.Context().Value(middleware.UserIDContextKey).(string)
	if !ok || strings.TrimSpace(userID) == "" {
		utils.RespondError(w, http.StatusUnauthorized, "Unauthorized: User ID not provided")
		return
	}

	var customerID, subscriptionID, subscriptionStatus sql.NullString
	var cancelAtPeriodEnd sql.NullBool

	err := s.Db.QueryRow(`
		SELECT stripe_customer_id, stripe_subscription_id, subscription_status, cancel_at_period_end
		FROM stripe
		WHERE user_id = $1
	`, userID).Scan(&customerID, &subscriptionID, &subscriptionStatus, &cancelAtPeriodEnd)
	if err != nil && err != sql.ErrNoRows {
		utils.RespondInternal(w, err, "Unable to look up billing account")
		return
	}

	hasActiveSubscription := false
	if subscriptionID.Valid && !cancelAtPeriodEnd.Bool {
		switch subscriptionStatus.String {
		case "active", "trialing", "past_due", "unpaid":
			hasActiveSubscription = true
		}
	}
	if hasActiveSubscription {
		utils.RespondError(w, http.StatusConflict, "You already have an active subscription")
		return
	}

	customerIDStr := strings.TrimSpace(customerID.String)

	params := &stripe.CheckoutSessionParams{
		SuccessURL:        stripe.String(s.FrontendURL + "/success?session_id={CHECKOUT_SESSION_ID}"),
		CancelURL:         stripe.String(s.FrontendURL + "/cancel"),
		Mode:              stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		ClientReferenceID: stripe.String(userID),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(priceID),
				Quantity: stripe.Int64(1),
			},
		},
		SubscriptionData: &stripe.CheckoutSessionSubscriptionDataParams{
			Metadata: map[string]string{
				"userID": userID,
			},
		},
		Metadata: map[string]string{
			"userID": userID,
		},
	}

	if customerIDStr != "" {
		params.Customer = stripe.String(customerIDStr)
	}

	result, err := session.New(params)
	if err != nil {
		utils.RespondInternal(w, err, "Unable to create checkout session")
		return
	}

	utils.RespondSuccess(w, http.StatusOK, map[string]string{"checkout_url": result.URL})
}

func (s *Stripe) CancelSubscription(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(middleware.UserIDContextKey).(string)
	if !ok || strings.TrimSpace(userID) == "" {
		utils.RespondError(w, http.StatusUnauthorized, "Unauthorized: User ID not provided")
		return
	}

	var subscriptionID sql.NullString
	err := s.Db.QueryRow(`SELECT stripe_subscription_id FROM stripe WHERE user_id = $1`, userID).Scan(&subscriptionID)
	if err != nil {
		if err == sql.ErrNoRows {
			utils.RespondError(w, http.StatusNotFound, "Subscription not found")
		} else {
			utils.RespondInternal(w, err, "Failed to fetch subscription")
		}
		return
	}
	if !subscriptionID.Valid || strings.TrimSpace(subscriptionID.String) == "" {
		utils.RespondError(w, http.StatusNotFound, "Subscription not found")
		return
	}

	_, err = subscription.Update(subscriptionID.String, &stripe.SubscriptionParams{
		CancelAtPeriodEnd: stripe.Bool(true),
	})
	if err != nil {
		utils.RespondInternal(w, err, "Failed to set subscription cancel at period end")
		return
	}

	result, err := s.Db.Exec(`
		UPDATE stripe
		SET cancel_at_period_end = true, updated_at = now()
		WHERE user_id = $1
	`, userID)
	if err != nil {
		utils.RespondInternal(w, err, "Failed to update subscription")
		return
	}

	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		utils.RespondError(w, http.StatusNotFound, "Subscription not found")
		return
	}

	utils.RespondSuccess(w, http.StatusOK, map[string]any{
		"status":               "cancel_at_period_end",
		"cancel_at_period_end": true,
	})
}

func (s *Stripe) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	const maxBodyBytes = int64(65536)
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	payload, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("webhook: error reading request body: %v", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	webhookSecret := os.Getenv("STRIPE_WEBHOOK_SECRET")
	if webhookSecret == "" {
		log.Printf("webhook: STRIPE_WEBHOOK_SECRET is not set, rejecting request")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	event, err := webhook.ConstructEvent(payload, r.Header.Get("Stripe-Signature"), webhookSecret)
	if err != nil {
		log.Printf("webhook: signature verification failed: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	switch event.Type {
	case "checkout.session.completed":
		err = utils.HandlePaymentSessionCompleted(s.Db, event)

	case "invoice.paid", "invoice.payment_succeeded":

		err = utils.HandleInvoicePaid(s.Db, event)

	case "invoice.payment_failed":
		err = utils.HandleInvoicePaymentFailed(s.Db, event)

	case "customer.subscription.created", "customer.subscription.updated":
		err = utils.HandleSubscriptionUpdated(s.Db, event)

	case "customer.subscription.deleted":
		err = utils.HandleSubscriptionDeleted(s.Db, event)

	default:
		log.Printf("webhook: unhandled event type: %s", event.Type)
	}

	if err != nil {
		log.Printf("webhook: failed to handle %s event %s: %v", event.Type, event.ID, err)
		http.Error(w, "webhook handler failed", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
