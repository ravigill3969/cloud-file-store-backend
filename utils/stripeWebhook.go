package utils

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/stripe/stripe-go/v85"
)

const (
	planTypeBasic = "basic"
	planTypePro   = "pro"

	basicPostAPICalls = 5
	basicGetAPICalls  = 5
	basicEditAPICalls = 5

	proPostAPICalls = 10
	proGetAPICalls  = 10
	proEditAPICalls = 10
)

var revokePremiumStatuses = map[stripe.SubscriptionStatus]bool{
	stripe.SubscriptionStatusUnpaid:            true,
	stripe.SubscriptionStatusCanceled:          true,
	stripe.SubscriptionStatusIncompleteExpired: true,
}

type sqlExecutor interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func applyUserPlan(exec sqlExecutor, userID, accountType string, postCalls, getCalls, editCalls int) error {
	result, err := exec.Exec(`
		UPDATE users
		SET post_api_calls = $1,
			get_api_calls = $2,
			edit_api_calls = $3,
			account_type = $4
		WHERE uuid = $5
	`, postCalls, getCalls, editCalls, accountType, userID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err == nil && rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

func grantPremium(exec sqlExecutor, userID string) error {
	return applyUserPlan(exec, userID, planTypePro, proPostAPICalls, proGetAPICalls, proEditAPICalls)
}

func revokePremium(exec sqlExecutor, userID string) error {
	return applyUserPlan(exec, userID, planTypeBasic, basicPostAPICalls, basicGetAPICalls, basicEditAPICalls)
}

func nullString(value string) sql.NullString {
	value = strings.TrimSpace(value)
	return sql.NullString{String: value, Valid: value != ""}
}

func nullTime(unixSeconds int64) sql.NullTime {
	if unixSeconds <= 0 {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: time.Unix(unixSeconds, 0), Valid: true}
}

func extractUserIDFromInvoice(inv *stripe.Invoice) string {
	if inv == nil {
		return ""
	}

	if inv.Parent != nil && inv.Parent.SubscriptionDetails != nil {
		if userID := strings.TrimSpace(inv.Parent.SubscriptionDetails.Metadata["userID"]); userID != "" {
			return userID
		}
	}

	if inv.Lines != nil && len(inv.Lines.Data) > 0 {
		for _, line := range inv.Lines.Data {
			if userID := strings.TrimSpace(line.Metadata["userID"]); userID != "" {
				return userID
			}
		}
	}

	return strings.TrimSpace(inv.Metadata["userID"])
}

func extractSubscriptionIDFromInvoice(inv *stripe.Invoice) string {
	if inv == nil {
		return ""
	}

	if inv.Parent != nil && inv.Parent.SubscriptionDetails != nil && inv.Parent.SubscriptionDetails.Subscription != nil {
		if subID := strings.TrimSpace(inv.Parent.SubscriptionDetails.Subscription.ID); subID != "" {
			return subID
		}
	}

	if inv.Lines != nil && len(inv.Lines.Data) > 0 {
		for _, line := range inv.Lines.Data {
			if line.Subscription != nil {
				if subID := strings.TrimSpace(line.Subscription.ID); subID != "" {
					return subID
				}
			}
			if line.Parent != nil && line.Parent.SubscriptionItemDetails != nil {
				if subID := strings.TrimSpace(line.Parent.SubscriptionItemDetails.Subscription); subID != "" {
					return subID
				}
			}
		}
	}

	return ""
}

func extractPriceIDFromInvoice(inv *stripe.Invoice) string {
	if inv == nil || inv.Lines == nil || len(inv.Lines.Data) == 0 {
		return ""
	}

	for _, line := range inv.Lines.Data {
		// In stripe-go v85 PriceDetails.Price is a *Price instead of a plain ID
		// string, and it can be nil, so it needs its own check.
		if line.Pricing == nil || line.Pricing.PriceDetails == nil || line.Pricing.PriceDetails.Price == nil {
			continue
		}

		if priceID := strings.TrimSpace(line.Pricing.PriceDetails.Price.ID); priceID != "" {
			return priceID
		}
	}

	return ""
}

func customerIDFromInvoice(inv *stripe.Invoice) string {
	if inv == nil || inv.Customer == nil {
		return ""
	}
	return strings.TrimSpace(inv.Customer.ID)
}

func customerIDFromSubscription(sub *stripe.Subscription) string {
	if sub == nil || sub.Customer == nil {
		return ""
	}
	return strings.TrimSpace(sub.Customer.ID)
}

func invoicePeriod(inv *stripe.Invoice) (time.Time, time.Time) {
	if inv == nil {
		return time.Time{}, time.Time{}
	}

	if inv.Lines != nil && len(inv.Lines.Data) > 0 && inv.Lines.Data[0].Period != nil {
		return time.Unix(inv.Lines.Data[0].Period.Start, 0), time.Unix(inv.Lines.Data[0].Period.End, 0)
	}

	return time.Unix(inv.PeriodStart, 0), time.Unix(inv.PeriodEnd, 0)
}

func lookupUserIDByStripeRefs(db *sql.DB, customerID, subscriptionID string) (string, error) {
	var userID string
	if subscriptionID != "" {
		if err := db.QueryRow(`
			SELECT user_id
			FROM stripe
			WHERE stripe_subscription_id = $1
		`, subscriptionID).Scan(&userID); err == nil {
			return userID, nil
		} else if err != sql.ErrNoRows {
			return "", err
		}
	}

	if customerID != "" {
		if err := db.QueryRow(`
			SELECT user_id
			FROM stripe
			WHERE stripe_customer_id = $1
		`, customerID).Scan(&userID); err == nil {
			return userID, nil
		} else if err != sql.ErrNoRows {
			return "", err
		}
	}

	return "", sql.ErrNoRows
}

func resolveUserIDForInvoice(db *sql.DB, inv *stripe.Invoice) (string, error) {
	if userID := extractUserIDFromInvoice(inv); userID != "" {
		return userID, nil
	}

	userID, err := lookupUserIDByStripeRefs(db, customerIDFromInvoice(inv), extractSubscriptionIDFromInvoice(inv))
	if err != nil {
		return "", fmt.Errorf("could not resolve user for invoice: %w", err)
	}

	return userID, nil
}

func resolveUserIDForSubscription(db *sql.DB, sub *stripe.Subscription) (string, error) {
	if sub == nil {
		return "", fmt.Errorf("subscription payload missing")
	}

	if userID := strings.TrimSpace(sub.Metadata["userID"]); userID != "" {
		return userID, nil
	}

	userID, err := lookupUserIDByStripeRefs(db, customerIDFromSubscription(sub), strings.TrimSpace(sub.ID))
	if err != nil {
		return "", fmt.Errorf("could not resolve user for subscription: %w", err)
	}

	return userID, nil
}

type stripeTxFunc func(tx *sql.Tx) error

// withTx runs fn inside a database transaction, rolling back on any error.
func withTx(db *sql.DB, fn stripeTxFunc) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin DB transaction: %w", err)
	}

	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit DB transaction: %w", err)
	}

	return nil
}

func HandleInvoicePaid(db *sql.DB, event stripe.Event) error {
	var inv stripe.Invoice
	if err := json.Unmarshal(event.Data.Raw, &inv); err != nil {
		return fmt.Errorf("failed to parse invoice paid event: %w", err)
	}

	if inv.Status != "" && inv.Status != stripe.InvoiceStatusPaid {
		log.Printf("invoice paid: ignoring invoice %s with status %q", inv.ID, inv.Status)
		return nil
	}

	userID, err := resolveUserIDForInvoice(db, &inv)
	if err != nil {
		return err
	}

	periodStart, periodEnd := invoicePeriod(&inv)
	customerID := customerIDFromInvoice(&inv)
	subscriptionID := extractSubscriptionIDFromInvoice(&inv)
	priceID := extractPriceIDFromInvoice(&inv)
	if priceID == "" {
		priceID = os.Getenv("STRIPE_PRICE_ID")
	}

	if err := withTx(db, func(tx *sql.Tx) error {
		if err := grantPremium(tx, userID); err != nil {
			return fmt.Errorf("failed to update user to pro: %w", err)
		}

		_, err := tx.Exec(`
			INSERT INTO stripe (
				user_id,
				stripe_customer_id,
				stripe_subscription_id,
				price_id,
				subscription_status,
				current_period_start,
				current_period_end,
				cancel_at_period_end
			)
			VALUES ($1, $2, $3, $4, 'active', $5, $6, false)
			ON CONFLICT (user_id)
			DO UPDATE SET
				subscription_status = 'active',
				price_id = EXCLUDED.price_id,
				current_period_start = EXCLUDED.current_period_start,
				current_period_end = EXCLUDED.current_period_end,
				canceled_at = NULL,
				stripe_customer_id = COALESCE(EXCLUDED.stripe_customer_id, stripe.stripe_customer_id),
				stripe_subscription_id = COALESCE(EXCLUDED.stripe_subscription_id, stripe.stripe_subscription_id),
				updated_at = now()
		`, userID, nullString(customerID), nullString(subscriptionID), priceID, periodStart, periodEnd)
		if err != nil {
			return fmt.Errorf("failed to save stripe record: %w", err)
		}

		return nil
	}); err != nil {
		return err
	}

	log.Printf("invoice paid handled for user %s", userID)
	return nil
}

func HandleInvoicePaymentFailed(db *sql.DB, event stripe.Event) error {
	var inv stripe.Invoice
	if err := json.Unmarshal(event.Data.Raw, &inv); err != nil {
		return fmt.Errorf("failed to parse invoice.payment_failed: %w", err)
	}

	userID, err := resolveUserIDForInvoice(db, &inv)
	if err != nil {
		return err
	}

	customerID := customerIDFromInvoice(&inv)
	subscriptionID := extractSubscriptionIDFromInvoice(&inv)
	finalFailure := inv.NextPaymentAttempt == 0

	return withTx(db, func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			UPDATE stripe
			SET subscription_status = 'past_due',
				stripe_customer_id = COALESCE($1, stripe_customer_id),
				stripe_subscription_id = COALESCE($2, stripe_subscription_id),
				updated_at = now()
			WHERE user_id = $3
		`, nullString(customerID), nullString(subscriptionID), userID)
		if err != nil {
			return fmt.Errorf("failed to mark stripe record past_due: %w", err)
		}

		if !finalFailure {
			log.Printf("invoice payment failed for user %s, next retry at %d", userID, inv.NextPaymentAttempt)
			return nil
		}

		if err := revokePremium(tx, userID); err != nil {
			return fmt.Errorf("failed to downgrade user to basic: %w", err)
		}

		log.Printf("invoice payment permanently failed for user %s", userID)
		return nil
	})
}

func HandlePaymentSessionCompleted(db *sql.DB, event stripe.Event) error {
	var session stripe.CheckoutSession
	if err := json.Unmarshal(event.Data.Raw, &session); err != nil {
		return fmt.Errorf("failed to parse checkout.session.completed: %w", err)
	}

	userID := strings.TrimSpace(session.Metadata["userID"])
	if userID == "" {
		userID = strings.TrimSpace(session.ClientReferenceID)
	}
	if userID == "" {
		return fmt.Errorf("userID not found in checkout session metadata")
	}

	customerID := ""
	if session.Customer != nil {
		customerID = strings.TrimSpace(session.Customer.ID)
	}

	subscriptionID := ""
	if session.Subscription != nil {
		subscriptionID = strings.TrimSpace(session.Subscription.ID)
	}

	priceID := os.Getenv("STRIPE_PRICE_ID")

	_, err := db.Exec(`
		INSERT INTO stripe (user_id, stripe_customer_id, stripe_subscription_id, price_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id)
		DO UPDATE SET
			stripe_customer_id = COALESCE(EXCLUDED.stripe_customer_id, stripe.stripe_customer_id),
			stripe_subscription_id = COALESCE(EXCLUDED.stripe_subscription_id, stripe.stripe_subscription_id),
			price_id = EXCLUDED.price_id,
			updated_at = now()
	`, userID, nullString(customerID), nullString(subscriptionID), priceID)
	if err != nil {
		return fmt.Errorf("failed to save stripe record: %w", err)
	}

	return nil
}

func HandleSubscriptionUpdated(db *sql.DB, event stripe.Event) error {
	var sub stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
		return fmt.Errorf("failed to parse subscription.updated: %w", err)
	}

	userID, err := resolveUserIDForSubscription(db, &sub)
	if err != nil {
		log.Printf("subscription updated: %v", err)
		return nil
	}

	canceledAt := nullTime(sub.CanceledAt)

	customerID := customerIDFromSubscription(&sub)

	return withTx(db, func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			UPDATE stripe
			SET subscription_status = $1,
				cancel_at_period_end = $2,
				canceled_at = $3,
				stripe_customer_id = COALESCE($4, stripe_customer_id),
				stripe_subscription_id = COALESCE($5, stripe_subscription_id),
				updated_at = now()
			WHERE user_id = $6
		`, string(sub.Status), sub.CancelAtPeriodEnd, canceledAt, nullString(customerID), nullString(sub.ID), userID)
		if err != nil {
			return fmt.Errorf("failed to update stripe record: %w", err)
		}

		if revokePremiumStatuses[sub.Status] {
			if err := revokePremium(tx, userID); err != nil {
				return fmt.Errorf("failed to downgrade user to basic: %w", err)
			}
		}

		return nil
	})
}

func HandleSubscriptionDeleted(db *sql.DB, event stripe.Event) error {
	var sub stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
		return fmt.Errorf("failed to parse subscription.deleted: %w", err)
	}

	userID, err := resolveUserIDForSubscription(db, &sub)
	if err != nil {
		return err
	}

	canceledAt := time.Now()
	if sub.CanceledAt > 0 {
		canceledAt = time.Unix(sub.CanceledAt, 0)
	}

	customerID := customerIDFromSubscription(&sub)

	return withTx(db, func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			UPDATE stripe
			SET subscription_status = 'canceled',
				cancel_at_period_end = false,
				canceled_at = $1,
				stripe_customer_id = COALESCE($2, stripe_customer_id),
				stripe_subscription_id = COALESCE($3, stripe_subscription_id),
				updated_at = now()
			WHERE user_id = $4
		`, canceledAt, nullString(customerID), nullString(sub.ID), userID)
		if err != nil {
			return fmt.Errorf("failed to update stripe record: %w", err)
		}

		if err := revokePremium(tx, userID); err != nil {
			return fmt.Errorf("failed to downgrade user to basic: %w", err)
		}

		return nil
	})
}
