package handlers

import "testing"

// I due campi deprecati devono dire la stessa cosa di quelli attuali.
//
// Sono serviti per non rompere chi già li legge, e una compatibilità che
// diverge dall'originale è peggio di nessuna compatibilità: un client vecchio
// continua a funzionare mostrando un numero che non corrisponde più a niente,
// e nessuno se ne accorge finché qualcuno non confronta due schermate.
//
// Il calcolo sta in un solo posto (SecurityOverview.setChecks) proprio perché
// non possa divergere. Questo test è la prova che ci resti.
func TestTheDeprecatedFieldsMirrorTheCurrentOnes(t *testing.T) {
	for _, tc := range []struct {
		name string
		b    ScoreBreakdown
	}{
		{"tutto spento", ScoreBreakdown{}},
		{"tutto acceso", ScoreBreakdown{
			AuditLogging: true, RBACEnabled: true, BackupFresh: true, RateLimiting: true,
			MFAAnyAdmin: true, AccountLockoutActive: true, StrongPasswordPolicy: true, MQTTTLS: true,
		}},
		{"installazione tipica", ScoreBreakdown{
			AuditLogging: true, RBACEnabled: true, AccountLockoutActive: true, StrongPasswordPolicy: true,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var o SecurityOverview
			o.setChecks(securityChecks(tc.b))

			if o.NIS2ChecksPassed != o.ChecksPassed {
				t.Errorf("nis2_checks_passed=%d ma checks_passed=%d: un client vecchio vedrebbe "+
					"un numero diverso da quello nuovo", o.NIS2ChecksPassed, o.ChecksPassed)
			}
			if o.NIS2ChecksTotal != o.ChecksEvaluated {
				t.Errorf("nis2_checks_total=%d ma checks_evaluated=%d", o.NIS2ChecksTotal, o.ChecksEvaluated)
			}
			if got := o.ChecksPassed + o.ChecksNotAssessed; got > len(o.Checks) {
				t.Errorf("i conteggi superano il numero di controlli: %d su %d", got, len(o.Checks))
			}
		})
	}
}

// Il denominatore non deve mai tornare a essere dodici.
//
// Prima lo era, e cinque di quei dodici erano true a codice: la frazione
// mostrata all'utente conteneva punti assegnati senza aver guardato niente, e
// tre punti irraggiungibili per costruzione. Se qualcuno rimette una costante
// al posto di una misura, questo test lo dice.
func TestOnlyMeasuredChecksAreCounted(t *testing.T) {
	// Con tutto spento, nessun controllo misurato può risultare superato.
	var off SecurityOverview
	off.setChecks(securityChecks(ScoreBreakdown{}))
	if off.ChecksPassed != 0 {
		t.Errorf("con ogni misura a false, %d controlli risultano comunque superati: "+
			"sono costanti, non misure", off.ChecksPassed)
	}

	// Con tutto acceso, i controlli misurati passano tutti e quelli
	// organizzativi restano non valutati — non diventano superati.
	var on SecurityOverview
	on.setChecks(securityChecks(ScoreBreakdown{
		AuditLogging: true, RBACEnabled: true, BackupFresh: true, RateLimiting: true,
		MFAAnyAdmin: true, AccountLockoutActive: true, StrongPasswordPolicy: true, MQTTTLS: true,
	}))
	if on.ChecksPassed != on.ChecksEvaluated {
		t.Errorf("con ogni misura a true, %d/%d superati: qualche controllo misurato "+
			"non segue lo stato della piattaforma", on.ChecksPassed, on.ChecksEvaluated)
	}
	if on.ChecksNotAssessed == 0 {
		t.Error("nessun controllo risulta non valutato: le misure organizzative sono " +
			"tornate a dichiararsi superate")
	}
	if on.ChecksEvaluated == len(on.Checks) {
		t.Errorf("tutti i %d controlli risultano valutati — il software non può "+
			"accertare gestione del rischio, incidenti, catena di fornitura, "+
			"crittografia, vulnerabilità e protezione dati", len(on.Checks))
	}
}

// Ogni voce del Security Center deve avere il testo del proprio verdetto.
//
// Il difetto era questo: la riga della sicurezza di rete diceva "MQTT non
// cifrato (TLS assente)" accanto a una spunta verde ogni volta che il TLS era
// attivo, perché il dettaglio era una costante indipendente dall'esito.
func TestEachComplianceDetailFollowsItsVerdict(t *testing.T) {
	fail := complianceChecks(false, false, false, "MFA non attivato su nessun admin")
	pass := complianceChecks(true, true, true, "2 admin con MFA abilitato")

	byID := func(list []ComplianceCheck, id string) ComplianceCheck {
		t.Helper()
		for _, c := range list {
			if c.ID == id {
				return c
			}
		}
		t.Fatalf("controllo %q sparito dall'elenco", id)
		return ComplianceCheck{}
	}

	for _, id := range []string{"business_continuity", "network_security", "mfa"} {
		f, p := byID(fail, id), byID(pass, id)
		if f.State != checkFail {
			t.Errorf("%s: con la misura a false lo stato è %q, non %q", id, f.State, checkFail)
		}
		if p.State != checkPass {
			t.Errorf("%s: con la misura a true lo stato è %q, non %q", id, p.State, checkPass)
		}
		if f.Detail == p.Detail {
			t.Errorf("%s: stesso dettaglio in entrambi i rami (%q) — è la costante che "+
				"faceva dire \"TLS assente\" accanto a una spunta verde", id, f.Detail)
		}
	}

	// E il campo deprecato deve seguire lo stato, in ogni voce.
	for _, list := range [][]ComplianceCheck{fail, pass} {
		for _, c := range list {
			if want := c.State == checkPass; c.Passed != want {
				t.Errorf("%s: passed=%v ma state=%q", c.ID, c.Passed, c.State)
			}
		}
	}
}
