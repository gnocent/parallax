package main

import (
	"os"
	"testing"
	"time"
)

func TestIntervalleSauvegarde(t *testing.T) {
	for _, cas := range []struct {
		texte  string
		veut   time.Duration
		erreur bool
	}{
		{"", 0, false}, {"0", 0, false}, {"24h", 24 * time.Hour, false}, {"6h30m", 6*time.Hour + 30*time.Minute, false},
		{"30s", 0, true}, {"demain", 0, true},
	} {
		d, err := intervalleSauvegarde(cas.texte)
		if (err != nil) != cas.erreur || d != cas.veut {
			t.Fatalf("%q : obtenu %s, %v", cas.texte, d, err)
		}
	}
}

func TestConfigS3DepuisEnvironnement(t *testing.T) {
	for _, nom := range []string{"PARALLAX_S3_ENDPOINT", "PARALLAX_S3_BUCKET", "PARALLAX_S3_CLE_ACCES", "PARALLAX_S3_CLE_SECRETE", "PARALLAX_S3_PREFIXE", "PARALLAX_S3_REGION"} {
		os.Unsetenv(nom)
	}
	if configS3DepuisEnvironnement().Configuree() {
		t.Fatal("sans variables, le dépôt S3 ne doit pas être configuré")
	}
	os.Setenv("PARALLAX_S3_ENDPOINT", "https://objets.interne:9000")
	os.Setenv("PARALLAX_S3_BUCKET", "sauvegardes")
	os.Setenv("PARALLAX_S3_CLE_ACCES", "AK")
	os.Setenv("PARALLAX_S3_CLE_SECRETE", "SK")
	defer func() {
		for _, nom := range []string{"PARALLAX_S3_ENDPOINT", "PARALLAX_S3_BUCKET", "PARALLAX_S3_CLE_ACCES", "PARALLAX_S3_CLE_SECRETE"} {
			os.Unsetenv(nom)
		}
	}()
	cfg := configS3DepuisEnvironnement()
	if !cfg.Configuree() || cfg.Prefixe != "parallax/" || cfg.Region != "us-east-1" || cfg.StyleVirtuel {
		t.Fatalf("configuration inattendue : %+v", cfg)
	}
}

func TestHeureCorrespond(t *testing.T) {
	for _, cas := range []struct {
		heure string
		cible string
		veut  bool
	}{
		{"02:00:00", "02:00", true},
		{"02:00:59", "02:00", true},
		{"02:01:00", "02:00", false},
		{"01:59:59", "02:00", false},
		{"14:30:00", "14:30", true},
		{"00:00:00", "00:00", true},
	} {
		maintenant, err := time.Parse("15:04:05", cas.heure)
		if err != nil {
			t.Fatal(err)
		}
		if got := heureCorrespond(maintenant, cas.cible); got != cas.veut {
			t.Fatalf("heureCorrespond(%s, %q) = %v, attendu %v", cas.heure, cas.cible, got, cas.veut)
		}
	}
}

func TestEnvOuDefaut(t *testing.T) {
	const nom = "PARALLAX_TEST_ENV_OU_DEFAUT"
	os.Unsetenv(nom)
	if v := envOuDefaut(nom, "defaut"); v != "defaut" {
		t.Fatalf("variable absente : attendu \"defaut\", obtenu %q", v)
	}

	os.Setenv(nom, "valeur-environnement")
	defer os.Unsetenv(nom)
	if v := envOuDefaut(nom, "defaut"); v != "valeur-environnement" {
		t.Fatalf("variable présente : attendu \"valeur-environnement\", obtenu %q", v)
	}

	os.Setenv(nom, "")
	if v := envOuDefaut(nom, "defaut"); v != "defaut" {
		t.Fatalf("variable vide : attendu le repli sur \"defaut\", obtenu %q", v)
	}
}
