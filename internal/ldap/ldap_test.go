package ldap

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// annuaireDeTest est un serveur LDAPS minimal : il lit un BindRequest,
// compare le DN et le mot de passe à ceux attendus, et répond success (0)
// ou invalidCredentials (49). Il enregistre le dernier DN reçu pour les
// assertions. Le certificat auto-signé est écrit en PEM dans un fichier
// temporaire, joué comme autorité interne (PARALLAX_LDAP_CA).
type annuaireDeTest struct {
	adresse   string
	fichierCA string
	dnAttendu string
	mdp       string
	dernierDN string
	binds     int
}

func demarrerAnnuaire(t *testing.T, dnAttendu, mdp string) *annuaireDeTest {
	t.Helper()
	cle, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	gabarit := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "annuaire.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:         true,
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, gabarit, gabarit, &cle.PublicKey, cle)
	if err != nil {
		t.Fatal(err)
	}
	fichierCA := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(fichierCA, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: cle}
	ecouteur, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ecouteur.Close() })

	a := &annuaireDeTest{adresse: ecouteur.Addr().String(), fichierCA: fichierCA, dnAttendu: dnAttendu, mdp: mdp}
	go func() {
		for {
			conn, err := ecouteur.Accept()
			if err != nil {
				return
			}
			go a.servir(conn)
		}
	}()
	return a
}

func (a *annuaireDeTest) servir(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	message, err := lireElement(conn, 0x30)
	if err != nil {
		return
	}
	idOctets, reste, err := decoderElement(message, 0x02)
	if err != nil {
		return
	}
	bind, _, err := decoderElement(reste, 0x60)
	if err != nil {
		return
	}
	_, apresVersion, err := decoderElement(bind, 0x02)
	if err != nil {
		return
	}
	dn, apresDN, err := decoderElement(apresVersion, 0x04)
	if err != nil {
		return
	}
	mdp, _, err := decoderElement(apresDN, 0x80)
	if err != nil {
		return
	}
	a.dernierDN = string(dn)
	a.binds++
	code := byte(49) // invalidCredentials
	if string(dn) == a.dnAttendu && string(mdp) == a.mdp {
		code = 0
	}
	// BindResponse : SEQUENCE { messageID, [APPLICATION 1] SEQUENCE { ENUMERATED code, OCTET STRING matchedDN, OCTET STRING message } }
	corps := concat([]byte{0x0a, 0x01, code}, []byte{0x04, 0x00}, []byte{0x04, 0x00})
	reponse := berSequence(0x30, concat(berChaine(0x02, idOctets), berSequence(0x61, corps)))
	_, _ = conn.Write(reponse)
}

func configVers(a *annuaireDeTest) Config {
	return Config{
		URL:       "ldaps://" + a.adresse,
		DNGabarit: "uid={login},ou=people,dc=exemple,dc=org",
		FichierCA: a.fichierCA,
		Delai:     3 * time.Second,
	}
}

func TestBindSimpleAccepteEtRefuse(t *testing.T) {
	a := demarrerAnnuaire(t, "uid=alice,ou=people,dc=exemple,dc=org", "s3cret!")
	client, err := NouveauClient(configVers(a))
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Lier("alice", "s3cret!"); err != nil {
		t.Fatalf("bind valide refusé : %v", err)
	}
	if a.dernierDN != "uid=alice,ou=people,dc=exemple,dc=org" {
		t.Fatalf("DN construit inattendu : %s", a.dernierDN)
	}
	if err := client.Lier("alice", "mauvais"); !errors.Is(err, ErrRefus) {
		t.Fatalf("mauvais mot de passe : attendu ErrRefus, obtenu %v", err)
	}
	if err := client.Lier("bob", "s3cret!"); !errors.Is(err, ErrRefus) {
		t.Fatalf("login inconnu : attendu ErrRefus, obtenu %v", err)
	}
	// mot de passe vide : refusé sans contacter l'annuaire (bind anonyme).
	binds := a.binds
	if err := client.Lier("alice", ""); !errors.Is(err, ErrRefus) || a.binds != binds {
		t.Fatalf("mot de passe vide : refus local attendu, obtenu %v (binds %d → %d)", err, binds, a.binds)
	}
}

func TestBindRefuseSansAutoriteReconnue(t *testing.T) {
	a := demarrerAnnuaire(t, "uid=alice,ou=people,dc=exemple,dc=org", "s3cret!")
	cfg := configVers(a)
	cfg.FichierCA = "" // pool système : le certificat auto-signé n'y est pas
	client, err := NouveauClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	err = client.Lier("alice", "s3cret!")
	if err == nil || errors.Is(err, ErrRefus) {
		t.Fatalf("une autorité inconnue doit être une panne TLS, pas un refus ni un succès : %v", err)
	}
}

func TestConfigValidation(t *testing.T) {
	cas := []struct {
		nom string
		cfg Config
		ok  bool
	}{
		{"ldap sans TLS", Config{URL: "ldap://annuaire:389", DNGabarit: "uid={login}"}, false},
		{"sans hôte", Config{URL: "ldaps://", DNGabarit: "uid={login}"}, false},
		{"gabarit sans login", Config{URL: "ldaps://annuaire", DNGabarit: "uid=x"}, false},
		{"CA introuvable", Config{URL: "ldaps://annuaire", DNGabarit: "uid={login}", FichierCA: "/nulle/part.pem"}, false},
		{"valide", Config{URL: "ldaps://annuaire", DNGabarit: "uid={login},dc=x"}, true},
	}
	for _, c := range cas {
		err := c.cfg.Valider()
		if (err == nil) != c.ok {
			t.Errorf("%s : attendu ok=%v, obtenu %v", c.nom, c.ok, err)
		}
	}
	if (Config{}).Configuree() || !(Config{URL: "ldaps://x"}).Configuree() {
		t.Fatal("Configuree suit la présence de l'URL")
	}
}

func TestDNEchappe(t *testing.T) {
	c := &Client{cfg: Config{DNGabarit: "uid={login},ou=people,dc=exemple,dc=org"}}
	if dn := c.DN("a,b=c"); dn != `uid=a\,b\=c,ou=people,dc=exemple,dc=org` {
		t.Fatalf("échappement DN inattendu : %s", dn)
	}
	if dn := c.DN(" x "); dn != `uid=\ x\ ,ou=people,dc=exemple,dc=org` {
		t.Fatalf("espaces de bord à échapper : %s", dn)
	}
}

func TestBERLongueursLongues(t *testing.T) {
	// un DN de plus de 127 octets force la forme longue de longueur, dans
	// le message comme dans la lecture.
	dn := "uid=" + strings.Repeat("a", 200) + ",dc=x"
	msg := EncoderBindRequest(300, dn, "mdp")
	contenu, err := lireElement(bytes.NewReader(msg), 0x30)
	if err != nil {
		t.Fatal(err)
	}
	id, reste, err := decoderElement(contenu, 0x02)
	if err != nil || !bytes.Equal(id, []byte{0x01, 0x2c}) {
		t.Fatalf("messageID 300 attendu en deux octets, obtenu %v (%v)", id, err)
	}
	bind, _, err := decoderElement(reste, 0x60)
	if err != nil {
		t.Fatal(err)
	}
	_, apresVersion, _ := decoderElement(bind, 0x02)
	lu, _, err := decoderElement(apresVersion, 0x04)
	if err != nil || string(lu) != dn {
		t.Fatalf("DN relu différent : %v", err)
	}
	// réponse avec un code non nul en deux octets et un message d'erreur
	reponse := berSequence(0x30, concat(berEntier(300), berSequence(0x61, concat([]byte{0x0a, 0x01, 0x31}, []byte{0x04, 0x00}, berChaine(0x04, []byte("invalid credentials"))))))
	code, err := LireBindResponse(bytes.NewReader(reponse))
	if err != nil || code != 49 {
		t.Fatalf("code 49 attendu, obtenu %d (%v)", code, err)
	}
	if _, err := LireBindResponse(bytes.NewReader(berSequence(0x30, concat(berEntier(1), berSequence(0x65, nil))))); err == nil {
		t.Fatal("une opération autre qu'un BindResponse doit être refusée")
	}
}
