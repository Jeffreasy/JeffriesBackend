package store

import (
	"strings"
	"testing"
)

func TestParseMailAccessCredentials(t *testing.T) {
	input := `Pilot Accounts:

E-mail: admin@example.test
Wachtwoord: ExamplePass1
Rol: Admin

E-mail: editor@example.test
Wachtwoord: ExamplePass2
Rol: Editor`

	credentials := parseMailAccessCredentials(input)
	if len(credentials) != 2 {
		t.Fatalf("expected 2 credentials, got %d", len(credentials))
	}
	if credentials[0].Email != "admin@example.test" || credentials[0].Password != "ExamplePass1" || credentials[0].Role != "Admin" {
		t.Fatalf("first credential parsed incorrectly: %#v", credentials[0])
	}
	if credentials[1].Email != "editor@example.test" || credentials[1].Password != "ExamplePass2" || credentials[1].Role != "Editor" {
		t.Fatalf("second credential parsed incorrectly: %#v", credentials[1])
	}
}

func TestParseMailAccessCredentialsInline(t *testing.T) {
	input := `Pilotaccounts: 1. E-mail: admin@example.test - Wachtwoord: ExamplePass1 - Rol: Admin 2. E-mail: editor@example.test - Wachtwoord: ExamplePass2 - Rol: Editor`

	credentials := parseMailAccessCredentials(input)
	if len(credentials) != 2 {
		t.Fatalf("expected 2 credentials, got %d: %#v", len(credentials), credentials)
	}
	if credentials[0].Email != "admin@example.test" || credentials[0].Password != "ExamplePass1" || credentials[0].Role != "Admin" {
		t.Fatalf("first inline credential parsed incorrectly: %#v", credentials[0])
	}
	if credentials[1].Email != "editor@example.test" || credentials[1].Password != "ExamplePass2" || credentials[1].Role != "Editor" {
		t.Fatalf("second inline credential parsed incorrectly: %#v", credentials[1])
	}
}

func TestFormatMailAccessCredentials(t *testing.T) {
	output := formatMailAccessCredentials([]mailAccessCredential{
		{Email: "admin@example.test", Password: "ExamplePass1", Role: "Admin"},
	})
	if output == "" {
		t.Fatal("expected formatted output")
	}
	if want := "Account 1\n- E-mail: admin@example.test\n- Wachtwoord: ExamplePass1\n- Rol: Admin"; !containsText(output, want) {
		t.Fatalf("expected readable account lines, got:\n%s", output)
	}
	if !isDefaultPilotAccessSummary("via het afgesproken veilige kanaal") {
		t.Fatal("expected legacy default access summary to be replaceable")
	}
}

func TestFormatMailAccessDetailsHTML(t *testing.T) {
	details := formatMailAccessDetails([]mailAccessCredential{
		{Email: "admin@example.test", Password: "ExamplePass1", Role: "Admin"},
	})
	if details.Intro == "" || details.Summary == "" || details.BlockHTML == "" {
		t.Fatalf("expected complete access details: %#v", details)
	}
	if !containsText(details.BlockHTML, "Pilotaccounts") || !containsText(details.BlockHTML, "mailto:admin@example.test") {
		t.Fatalf("expected structured access HTML, got:\n%s", details.BlockHTML)
	}
	if containsText(details.BlockHTML, "E-mail: admin@example.test - Wachtwoord") {
		t.Fatalf("expected access HTML to avoid flat inline credential text:\n%s", details.BlockHTML)
	}
}

func containsText(value, needle string) bool {
	return len(needle) == 0 || (len(value) >= len(needle) && strings.Contains(value, needle))
}
