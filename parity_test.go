package xdk

import "testing"

func TestGeneratedCoverage(t *testing.T) {
	if got, want := len(generatedTags), 18; got != want {
		t.Fatalf("generatedTags mismatch: got %d want %d", got, want)
	}
	if got, want := len(operations), 142; got != want {
		t.Fatalf("operations mismatch: got %d want %d", got, want)
	}
}

func TestClientWiresAllGeneratedClients(t *testing.T) {
	c := NewClient(Config{})
	if c.AccountActivity == nil || c.Activity == nil || c.Communities == nil || c.CommunityNotes == nil || c.Compliance == nil || c.Connections == nil || c.DirectMessages == nil || c.General == nil || c.Lists == nil || c.Media == nil || c.News == nil || c.Posts == nil || c.Spaces == nil || c.Stream == nil || c.Trends == nil || c.Usage == nil || c.Users == nil || c.Webhooks == nil {
		t.Fatal("one or more generated clients are nil")
	}
}
