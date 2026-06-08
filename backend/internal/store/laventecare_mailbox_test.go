package store

import "testing"

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

func TestFormatMailAccessCredentials(t *testing.T) {
	output := formatMailAccessCredentials([]mailAccessCredential{
		{Email: "admin@example.test", Password: "ExamplePass1", Role: "Admin"},
	})
	if output == "" {
		t.Fatal("expected formatted output")
	}
	if !isDefaultPilotAccessSummary("via het afgesproken veilige kanaal") {
		t.Fatal("expected legacy default access summary to be replaceable")
	}
}
