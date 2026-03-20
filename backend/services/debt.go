package services

import (
	"container/heap"
	"math"
	"syncpay/database"
	"syncpay/models"
)

// ---- Heap implementation for debt simplification ----

type debtEntry struct {
	amount   int
	username string
}

type debtHeap []debtEntry

func (h debtHeap) Len() int            { return len(h) }
func (h debtHeap) Less(i, j int) bool  { return h[i].amount < h[j].amount }
func (h debtHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *debtHeap) Push(x interface{}) { *h = append(*h, x.(debtEntry)) }
func (h *debtHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

// ProcessNewDebt handles a new simple debt (from owes to)
func ProcessNewDebt(groupID uint, fromUser, toUser string, amount int) {
	// Step 1: Update UserDebt
	updateUserDebt(groupID, fromUser, amount)
	updateUserDebt(groupID, toUser, -amount)

	// Steps 2-3: Update pairwise debts
	addPairwiseDebt(groupID, fromUser, toUser, amount)
}

// ReverseDebt handles settling a debt (from pays to)
func ReverseDebt(groupID uint, fromUser, toUser string, amount int) {
	// Step 1: Update UserDebt (opposite direction)
	updateUserDebt(groupID, fromUser, -amount)
	updateUserDebt(groupID, toUser, amount)

	// Steps 2-3: Reverse pairwise debts
	reversePairwiseDebt(groupID, fromUser, toUser, amount)
}

// ProcessMultiPayerDebt handles multi-payer expense creation
func ProcessMultiPayerDebt(groupID uint, lenders []LenderBorrower, borrowers []LenderBorrower, totalAmount int) {
	// Step 1: Update UserDebt for each lender and borrower
	for _, l := range lenders {
		updateUserDebt(groupID, l.Username, -l.Amount) // paid, so owed
	}
	for _, b := range borrowers {
		updateUserDebt(groupID, b.Username, b.Amount) // owes
	}

	// Step 2: Create proportional pairwise debts
	for _, b := range borrowers {
		for _, l := range lenders {
			if b.Username == l.Username {
				continue
			}
			pairAmount := int(math.Round(float64(b.Amount) * float64(l.Amount) / float64(totalAmount)))
			if pairAmount > 0 {
				addPairwiseDebt(groupID, b.Username, l.Username, pairAmount)
			}
		}
	}
}

// ReverseMultiPayerDebt reverses a multi-payer expense (for delete/edit)
func ReverseMultiPayerDebt(groupID uint, lenders []LenderBorrower, borrowers []LenderBorrower, totalAmount int) {
	// Step 1: Reverse UserDebt
	for _, l := range lenders {
		updateUserDebt(groupID, l.Username, l.Amount)
	}
	for _, b := range borrowers {
		updateUserDebt(groupID, b.Username, -b.Amount)
	}

	// Step 2: Reverse proportional pairwise debts
	for _, b := range borrowers {
		for _, l := range lenders {
			if b.Username == l.Username {
				continue
			}
			pairAmount := int(math.Round(float64(b.Amount) * float64(l.Amount) / float64(totalAmount)))
			if pairAmount > 0 {
				reversePairwiseDebt(groupID, b.Username, l.Username, pairAmount)
			}
		}
	}
}

// SimplifyDebts runs the greedy transaction minimization algorithm
func SimplifyDebts(groupID uint) {
	var userDebts []models.UserDebt
	database.DB.Where("group_id = ?", groupID).Find(&userDebts)

	debtors := &debtHeap{}
	creditors := &debtHeap{}
	heap.Init(debtors)
	heap.Init(creditors)

	for _, ud := range userDebts {
		if ud.NetDebt > 0 {
			heap.Push(debtors, debtEntry{amount: ud.NetDebt, username: ud.Username})
		} else if ud.NetDebt < 0 {
			heap.Push(creditors, debtEntry{amount: -ud.NetDebt, username: ud.Username})
		}
	}

	// Delete all existing optimised debts
	database.DB.Where("group_id = ?", groupID).Delete(&models.OptimisedDebt{})

	for debtors.Len() > 0 && creditors.Len() > 0 {
		debtor := heap.Pop(debtors).(debtEntry)
		creditor := heap.Pop(creditors).(debtEntry)

		transaction := debtor.amount
		if creditor.amount < transaction {
			transaction = creditor.amount
		}

		database.DB.Create(&models.OptimisedDebt{
			GroupID:  groupID,
			FromUser: debtor.username,
			ToUser:   creditor.username,
			Amount:   transaction,
		})

		debtorRemainder := debtor.amount - transaction
		creditorRemainder := creditor.amount - transaction

		if debtorRemainder > 0 {
			heap.Push(debtors, debtEntry{amount: debtorRemainder, username: debtor.username})
		}
		if creditorRemainder > 0 {
			heap.Push(creditors, debtEntry{amount: creditorRemainder, username: creditor.username})
		}
	}
}

// LenderBorrower represents a username-amount pair
type LenderBorrower struct {
	Username string
	Amount   int
}

// ---- Internal helpers ----

func updateUserDebt(groupID uint, username string, delta int) {
	var ud models.UserDebt
	result := database.DB.Where("group_id = ? AND username = ?", groupID, username).First(&ud)
	if result.Error != nil {
		ud = models.UserDebt{GroupID: groupID, Username: username, NetDebt: delta}
		database.DB.Create(&ud)
	} else {
		database.DB.Model(&ud).Update("net_debt", ud.NetDebt+delta)
	}
}

func addPairwiseDebt(groupID uint, fromUser, toUser string, amount int) {
	// Check for reverse debt
	var reverse models.Debt
	result := database.DB.Where("group_id = ? AND from_user = ? AND to_user = ?", groupID, toUser, fromUser).First(&reverse)

	if result.Error == nil {
		// Reverse debt exists
		if reverse.Amount > amount {
			database.DB.Model(&reverse).Update("amount", reverse.Amount-amount)
			return
		}
		remaining := amount - reverse.Amount
		database.DB.Delete(&reverse)
		amount = remaining
		if amount == 0 {
			return
		}
	}

	// Create or update forward debt
	var existing models.Debt
	result = database.DB.Where("group_id = ? AND from_user = ? AND to_user = ?", groupID, fromUser, toUser).First(&existing)
	if result.Error == nil {
		database.DB.Model(&existing).Update("amount", existing.Amount+amount)
	} else {
		database.DB.Create(&models.Debt{
			GroupID:  groupID,
			FromUser: fromUser,
			ToUser:   toUser,
			Amount:   amount,
		})
	}
}

func reversePairwiseDebt(groupID uint, fromUser, toUser string, amount int) {
	// Check for existing forward debt
	var existing models.Debt
	result := database.DB.Where("group_id = ? AND from_user = ? AND to_user = ?", groupID, fromUser, toUser).First(&existing)

	if result.Error == nil {
		if existing.Amount > amount {
			database.DB.Model(&existing).Update("amount", existing.Amount-amount)
			return
		}
		remaining := amount - existing.Amount
		database.DB.Delete(&existing)
		amount = remaining
		if amount == 0 {
			return
		}
	}

	// Create or update reverse debt
	var reverse models.Debt
	result = database.DB.Where("group_id = ? AND from_user = ? AND to_user = ?", groupID, toUser, fromUser).First(&reverse)
	if result.Error == nil {
		database.DB.Model(&reverse).Update("amount", reverse.Amount+amount)
	} else {
		database.DB.Create(&models.Debt{
			GroupID:  groupID,
			FromUser: toUser,
			ToUser:   fromUser,
			Amount:   amount,
		})
	}
}
