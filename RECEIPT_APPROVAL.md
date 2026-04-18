# Receipt Approval Workflow (Подтверждение Чеков)

## Overview
This document describes the receipt approval workflow that requires admin confirmation for Kaspi Red payment receipts.

## Flow Diagram

```
CLIENT UPLOADS RECEIPT (Kaspi Red payment)
           ↓
Receipt saved to S3, status = "pending"
           ↓
Admin notified with receipt image preview
           ↓
ADMIN CHOICES:
  ├→ ✅ APPROVE → receipt_status = "confirmed"
  │                Status = "confirmed"
  │                Client notified: "✅ Чек подтвержден!"
  │
  └→ ❌ REJECT → receipt_status = "rejected"
                 Client notified: "❌ Чек отклонен, загрузите новый"
                 Client state set back to "StateAwaitingReceipt"
                 New 30-min deadline scheduled
```

## Database Changes

### Appointments Table
Added new column:
```sql
ALTER TABLE appointments ADD COLUMN IF NOT EXISTS receipt_status TEXT DEFAULT 'pending';
```

### Values
- `null` / empty: No receipt uploaded (payment_type = 'cash' or Kaspi without receipt)
- `'pending'`: Receipt uploaded, awaiting admin approval
- `'confirmed'`: Admin approved, appointment confirmed
- `'rejected'`: Admin rejected, client must re-upload

## Code Changes

### 1. models/models.go
Added receipt status constants:
```go
const (
    ReceiptStatusPending   = "pending"
    ReceiptStatusConfirmed = "confirmed"
    ReceiptStatusRejected  = "rejected"
)
```

### 2. handlers/client.go
**Updated `createAppointment()` function:**
- Now sets `receipt_status = "pending"` when Kaspi payment with receipt
- Passes `appointmentID` to notification function
- Calls `notifyAdminNewAppointment()` with receipt flag

**Updated `notifyAdminNewAppointment()` function:**
- Added parameters: `appointmentID`, `hasReceipt`
- When `hasReceipt = true`: Adds inline buttons for ✅ Approve / ❌ Reject
- Buttons use callback format: `confirm_receipt_{appointmentID}`, `reject_receipt_{appointmentID}`

### 3. handlers/admin.go
Added two new handler functions:

**`HandleConfirmReceipt(c telebot.Context, appointmentID int)`:**
- Updates `receipt_status = 'confirmed'`
- Sends notification to client: "✅ Ваш чек подтвержден!"
- Returns confirmation message in admin chat

**`HandleRejectReceipt(c telebot.Context, appointmentID int)`:**
- Updates `receipt_status = 'rejected'`
- Sends notification to client: "❌ Чек отклонен. Загрузите новый."
- No automatic state reset (client can re-upload within deadline)

### 4. main.go
Added callback handlers for receipt approval:
```go
// Подтверждение чека администратором
if len(data) > 15 && data[:15] == "confirm_receipt_" {
    // Parse appointmentID and call HandleConfirmReceipt()
}

// Отклонение чека администратором
if len(data) > 14 && data[:14] == "reject_receipt_" {
    // Parse appointmentID and call HandleRejectReceipt()
}
```

## Workflow Example

### Client Side
1. Client books appointment → selects Kaspi Red payment
2. Gets redirected to Kaspi payment page
3. Returns and uploads receipt screenshot
4. System sends receipt to admin for approval
5. Awaits admin decision (has 30 min deadline to retry if rejected)

### Admin Side
1. Receives notification: "🔔 Новая запись! ... 📄 Требуется подтверждение чека!"
2. Sees two buttons: "✅ Подтвердить чек" and "❌ Отклонить"
3. Clicks one of them
4. System updates status and notifies client

## States Involved
- `StateAwaitingReceipt`: Client awaiting receipt upload (both initial and after rejection)
- Receipt deadline: 30 minutes (managed by scheduler)
- If deadline expires and no receipt: appointment auto-cancelled

## Testing Checklist
- [ ] Kaspi Red payment → receipt upload triggers admin notification
- [ ] Admin can see receipt image in notification
- [ ] ✅ Approve → client notified, appointment confirmed
- [ ] ❌ Reject → client notified, can re-upload
- [ ] Cash payments bypass receipt workflow
- [ ] Prepay percentage correctly calculated
- [ ] Reminder scheduled after approval
- [ ] Database migration for receipt_status works

## Future Enhancements
- Receipt image preview in admin UI (currently Telegram-native)
- Rejection reason feedback to client
- Receipt re-upload attempt counter
- Statistics: approval rate, rejection rate
- Batch approval for multiple receipts
