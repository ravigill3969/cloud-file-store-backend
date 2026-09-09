package utils

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/stripe/stripe-go/v82"
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
		if line.Pricing != nil && line.Pricing.PriceDetails != nil {
			if priceID := strings.TrimSpace(line.Pricing.PriceDetails.Price); priceID != "" {
				return priceID
			}
		}
	}

	return ""
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

	customerID := ""
	if inv != nil && inv.Customer != nil {
		customerID = strings.TrimSpace(inv.Customer.ID)
	}

	subscriptionID := extractSubscriptionIDFromInvoice(inv)
	userID, err := lookupUserIDByStripeRefs(db, customerID, subscriptionID)
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

	customerID := ""
	if sub.Customer != nil {
		customerID = strings.TrimSpace(sub.Customer.ID)
	}

	userID, err := lookupUserIDByStripeRefs(db, customerID, strings.TrimSpace(sub.ID))
	if err != nil {
		return "", fmt.Errorf("could not resolve user for subscription: %w", err)
	}

	return userID, nil
}

func HandleInvoicePaid(db *sql.DB, event stripe.Event) error {
	var inv stripe.Invoice

	if err := json.Unmarshal(event.Data.Raw, &inv); err != nil {
		return fmt.Errorf("failed to parse invoice.payment_succeeded: %w", err)
	}

	userID, err := resolveUserIDForInvoice(db, &inv)
	if err != nil {
		return err
	}

	periodStart := time.Unix(inv.PeriodStart, 0)
	periodEnd := time.Unix(inv.PeriodEnd, 0)
	if inv.Lines != nil && len(inv.Lines.Data) > 0 && inv.Lines.Data[0].Period != nil {
		periodStart = time.Unix(inv.Lines.Data[0].Period.Start, 0)
		periodEnd = time.Unix(inv.Lines.Data[0].Period.End, 0)
	}

	customerID := ""
	if inv.Customer != nil {
		customerID = strings.TrimSpace(inv.Customer.ID)
	}

	subscriptionID := extractSubscriptionIDFromInvoice(&inv)
	priceID := extractPriceIDFromInvoice(&inv)
	if priceID == "" {
		priceID = os.Getenv("STRIPE_PRICE_ID")
	}

	if err := applyUserPlan(db, userID, planTypePro, proPostAPICalls, proGetAPICalls, proEditAPICalls); err != nil {
		return fmt.Errorf("failed to update user to pro: %w", err)
	}

	_, err = db.Exec(`
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
		VALUES (
			$1,
			NULLIF($2, ''),
			NULLIF($3, ''),
			$4,
			'active',
			$5,
			$6,
			false
		)
		ON CONFLICT (user_id)
		DO UPDATE SET
			stripe_customer_id = COALESCE(NULLIF(EXCLUDED.stripe_customer_id, ''), stripe.stripe_customer_id),
			stripe_subscription_id = COALESCE(NULLIF(EXCLUDED.stripe_subscription_id, ''), stripe.stripe_subscription_id),
			price_id = EXCLUDED.price_id,
			subscription_status = EXCLUDED.subscription_status,
			current_period_start = EXCLUDED.current_period_start,
			current_period_end = EXCLUDED.current_period_end,
			cancel_at_period_end = EXCLUDED.cancel_at_period_end,
			updated_at = now()
	`, userID, customerID, subscriptionID, priceID, periodStart, periodEnd)
	if err != nil {
		return fmt.Errorf("failed to upsert stripe record: %w", err)
	}

	log.Printf("invoice paid handled for user %s", userID)
	return nil
}

func HandlePaymentSessionCompleted(db *sql.DB, event stripe.Event) error {
	fmt.Println("createting session")

	var session stripe.CheckoutSession
	err := json.Unmarshal(event.Data.Raw, &session)
	if err != nil {
		return fmt.Errorf("something went wrong")

	}
	userID := session.Metadata["userID"]
	priceId := os.Getenv("STRIPE_PRICE_ID")
	subscriptionID := ""
	if session.Subscription != nil {
		subscriptionID = strings.TrimSpace(session.Subscription.ID)
	}
	customerID := ""
	if session.Customer != nil {
		customerID = strings.TrimSpace(session.Customer.ID)
	}

	if strings.TrimSpace(userID) == "" {
		return fmt.Errorf("userID not found in checkout session metadata")
	}

	_, err = db.Exec(`
		INSERT INTO stripe (user_id, stripe_customer_id, stripe_subscription_id, price_id)
		VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), $4)
		ON CONFLICT (user_id)
		DO UPDATE SET
			stripe_customer_id = COALESCE(NULLIF(EXCLUDED.stripe_customer_id, ''), stripe.stripe_customer_id),
			stripe_subscription_id = COALESCE(NULLIF(EXCLUDED.stripe_subscription_id, ''), stripe.stripe_subscription_id),
			price_id = EXCLUDED.price_id,
			updated_at = now()
	`, userID, customerID, subscriptionID, priceId)

	return err
}

func HandleSubscriptionUpdated(db *sql.DB, event stripe.Event) error {
	var sub stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
		return fmt.Errorf("failed to parse subscription.updated: %w", err)
	}

	customerID := ""
	if sub.Customer != nil {
		customerID = strings.TrimSpace(sub.Customer.ID)
	}

	if customerID == "" && strings.TrimSpace(sub.ID) == "" {
		return fmt.Errorf("subscription.updated missing identifiers")
	}

	status := string(sub.Status)

	_, err := db.Exec(`
		UPDATE stripe
		SET subscription_status = $1,
			cancel_at_period_end = $2,
			canceled_at = CASE WHEN $2 = true THEN now() ELSE NULL END
		WHERE stripe_customer_id = $3 OR stripe_subscription_id = $4
	`, status, sub.CancelAtPeriodEnd, customerID, sub.ID)

	if err != nil {
		return fmt.Errorf("failed to update stripe record: %w", err)
	}

	return nil
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

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin DB transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		} else {
			_ = tx.Commit()
		}
	}()

	_, err = tx.Exec(`
		UPDATE stripe
		SET subscription_status = 'canceled',
			cancel_at_period_end = false,
			canceled_at = now()
		WHERE user_id = $1
	`, userID)
	if err != nil {
		return fmt.Errorf("failed to update stripe record: %w", err)
	}

	err = applyUserPlan(tx, userID, planTypeBasic, basicPostAPICalls, basicGetAPICalls, basicEditAPICalls)
	if err != nil {
		return fmt.Errorf("failed to downgrade user to basic: %w", err)
	}

	return nil
}
