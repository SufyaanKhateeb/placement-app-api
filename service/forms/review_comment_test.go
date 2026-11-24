package forms

import "testing"

func TestReadLegacyReviewFeedback(t *testing.T) {
	var receipt submission
	if err := receipt.readReview([]byte(`{"email":"Use your work address"}`)); err != nil {
		t.Fatal(err)
	}
	if len(receipt.Comment) != 0 || receipt.Changes["email"] != "Use your work address" {
		t.Fatalf("legacy feedback did not round-trip: %+v", receipt)
	}
}
