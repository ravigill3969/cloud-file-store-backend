## Webhook Purpose & Actions

### **🟢 GIVE Premium Features:**
- **`invoice.payment_succeeded`** - Payment actually went through → **ACTIVATE premium features**

### **🔴 REVOKE Premium Features:**
- **`invoice.payment_failed`** - Payment failed → **REVOKE premium features + notify user**
- **`customer.subscription.deleted`** - Subscription canceled → **REVOKE premium features**
- **`customer.subscription.updated`** when status is:
  - `past_due` → **REVOKE premium features**
  - `canceled` → **REVOKE premium features**
  - `unpaid` → **REVOKE premium features**
  - `incomplete_expired` → **REVOKE premium features**

### **📝 Record Keeping Only (No Feature Changes):**
- **`checkout.session.completed`** - User completed checkout → Create subscription record + send welcome email
- **`customer.subscription.created`** - Subscription object created → Update database record
- **`customer.subscription.updated`** - Subscription details changed → Update database record

### **🔔 Notifications Only:**
- **`customer.subscription.updated`** (when `cancel_at_period_end = true`) → Send "subscription will cancel" notice
- **`customer.subscription.deleted`** → Send cancellation confirmation email

## **Key Rule:**
- **Only 1 webhook gives premium features:** `invoice.payment_succeeded`
- **Multiple webhooks revoke premium features:** payment failures and cancellations
- **Everything else is just housekeeping:** database updates and emails

The reason is simple: `invoice.payment_succeeded` is the only guarantee that money actually changed hands. Everything else is just Stripe telling you about status changes.