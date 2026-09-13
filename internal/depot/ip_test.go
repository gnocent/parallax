package depot

import (
	"errors"
	"strings"
	"testing"
)

func TestParserIPv4(t *testing.T) {
	cas := []struct {
		texte  string
		valeur uint32
		ok     bool
	}{
		{"10.0.0.1", 0x0A000001, true},
		{" 192.168.1.254 ", 0xC0A801FE, true},
		{"0.0.0.0", 0, true},
		{"255.255.255.255", 0xFFFFFFFF, true},
		{"", 0, false},
		{"10.0.0", 0, false},
		{"10.0.0.256", 0, false},
		{"10.0.0.1.2", 0, false},
		{"abc", 0, false},
		{"::1", 0, false},
		{"2001:db8::1", 0, false},
		{"::ffff:10.0.0.1", 0, false},
	}
	for _, c := range cas {
		v, err := ParserIPv4(c.texte)
		if c.ok {
			if err != nil || v != c.valeur {
				t.Errorf("%q : attendu %#x, obtenu %#x, %v", c.texte, c.valeur, v, err)
			}
			continue
		}
		if err == nil || !errors.Is(err, ErrValidation) {
			t.Errorf("%q : ErrValidation attendue, obtenu %v", c.texte, err)
		}
	}
}

func TestParserIPv4RefuseIPv6AvecUnMessageClair(t *testing.T) {
	_, err := ParserIPv4("2001:db8::1")
	if err == nil || !errors.Is(err, ErrValidation) {
		t.Fatalf("ErrValidation attendue, obtenu %v", err)
	}
	if msg := err.Error(); !strings.Contains(msg, "IPv6") || !strings.Contains(msg, "hors périmètre") {
		t.Fatalf("le message doit nommer IPv6 et le périmètre : %s", msg)
	}
}

func TestFormaterIPv4EstInverseDeParser(t *testing.T) {
	for _, s := range []string{"10.0.0.1", "0.0.0.0", "255.255.255.255", "172.16.254.3"} {
		v, err := ParserIPv4(s)
		if err != nil {
			t.Fatal(err)
		}
		if FormaterIPv4(v) != s {
			t.Errorf("%s -> %#x -> %s", s, v, FormaterIPv4(v))
		}
	}
	if NormaliserIPv4(" 10.0.0.1 ") != "10.0.0.1" || NormaliserIPv4(" pas-une-ip ") != "pas-une-ip" {
		t.Fatal("NormaliserIPv4 : forme canonique si parsable, texte recadré sinon")
	}
}

func TestComparaisonNumeriquePasLexicale(t *testing.T) {
	a, _ := ParserIPv4("10.0.0.9")
	b, _ := ParserIPv4("10.0.0.10")
	if ComparerIPv4(a, b) != -1 || ComparerIPv4(b, a) != 1 || ComparerIPv4(a, a) != 0 {
		t.Fatal("10.0.0.9 doit précéder 10.0.0.10")
	}
}

func TestIntervalleIPv4(t *testing.T) {
	i, err := ParserIntervalleIPv4("10.0.0.10", "10.0.0.20")
	if err != nil {
		t.Fatal(err)
	}
	if i.Taille() != 11 {
		t.Fatalf("taille attendue 11, obtenu %d", i.Taille())
	}
	for _, c := range []struct {
		ip string
		in bool
	}{{"10.0.0.9", false}, {"10.0.0.10", true}, {"10.0.0.15", true}, {"10.0.0.20", true}, {"10.0.0.21", false}} {
		v, _ := ParserIPv4(c.ip)
		if i.Contient(v) != c.in {
			t.Errorf("%s dans %v : attendu %v", c.ip, i, c.in)
		}
	}
	j, _ := ParserIntervalleIPv4("10.0.0.20", "10.0.0.30")
	k, _ := ParserIntervalleIPv4("10.0.0.21", "10.0.0.30")
	if !i.Chevauche(j) || !j.Chevauche(i) || i.Chevauche(k) || k.Chevauche(i) {
		t.Fatal("chevauchement : bornes comprises")
	}

	if _, err := ParserIntervalleIPv4("10.0.0.20", "10.0.0.10"); !errors.Is(err, ErrValidation) {
		t.Fatalf("début > fin doit être refusé, obtenu %v", err)
	}
	if _, err := ParserIntervalleIPv4("10.0.0.1", "x"); !errors.Is(err, ErrValidation) {
		t.Fatalf("borne invalide doit être refusée, obtenu %v", err)
	}

	// parcours sans débordement jusqu'à la dernière adresse de l'espace
	fin := IntervalleIPv4{Debut: 0xFFFFFFFE, Fin: 0xFFFFFFFF}
	var vus []uint32
	fin.Parcourir(func(v uint32) bool { vus = append(vus, v); return true })
	if len(vus) != 2 || vus[1] != 0xFFFFFFFF {
		t.Fatalf("parcours attendu de deux adresses, obtenu %v", vus)
	}
	var n int
	i.Parcourir(func(uint32) bool { n++; return n < 3 })
	if n != 3 {
		t.Fatalf("le parcours doit s'arrêter quand fn renvoie faux, obtenu %d appels", n)
	}
}
