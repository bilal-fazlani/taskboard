package models

import (
	"reflect"
	"testing"
	"time"
)

// UserInputTypes is the one list a request's type is checked against.
func TestUserInputTypesAreApprovalAndQuestion(t *testing.T) {
	if want := []string{"approval", "question"}; !reflect.DeepEqual(UserInputTypes, want) {
		t.Fatalf("UserInputTypes = %v, want %v", UserInputTypes, want)
	}
	for _, typ := range UserInputTypes {
		if !ValidUserInputType(typ) {
			t.Errorf("ValidUserInputType(%q) = false, want true", typ)
		}
	}
	for _, typ := range []string{"", "Approval", " question", "needs_approval", "hand_off"} {
		if ValidUserInputType(typ) {
			t.Errorf("ValidUserInputType(%q) = true, want false", typ)
		}
	}
}

func TestTicketRequestIsAnsweredOnceItHasAnAnswerTime(t *testing.T) {
	r := TicketRequest{Type: UserInputQuestion, Prompt: "Which one?"}
	if r.Answered() {
		t.Fatal("a request with no answer time reads as answered")
	}
	at := time.Now()
	r.Answer, r.AnsweredBy, r.AnsweredAt = "the first", "bilal", &at
	if !r.Answered() {
		t.Fatal("a request with an answer time reads as unanswered")
	}
}
