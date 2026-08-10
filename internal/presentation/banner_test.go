package presentation

import (
	"reflect"
	"testing"
)

func TestAgentBannerTextIncludesSessionID(t *testing.T) {
	t.Parallel()

	sessionID := callID(0x5A)
	tests := []struct {
		name   string
		banner AgentBanner
		want   string
	}{
		{name: "name and description", banner: AgentBanner{Name: "Carbon", Description: "coding rig"}, want: "Carbon — coding rig\nSession: #" + sessionID.String()},
		{name: "name only", banner: AgentBanner{Name: "Carbon"}, want: "Carbon\nSession: #" + sessionID.String()},
		{name: "description only", banner: AgentBanner{Description: "coding rig"}, want: "coding rig\nSession: #" + sessionID.String()},
		{name: "fallback", banner: AgentBanner{}, want: "session ready\nSession: #" + sessionID.String()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.banner.bannerText(sessionID); got != tt.want {
				t.Errorf("bannerText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAgentBannerHasNoGreetingField(t *testing.T) {
	t.Parallel()
	if _, ok := reflect.TypeOf(AgentBanner{}).FieldByName("Greeting"); ok {
		t.Fatal("AgentBanner still exposes removed Greeting field")
	}
}
