package sauvegarde

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- Signature AWS Signature Version 4, contre les exemples officiels ---
//
// Les deux tests suivants rejouent, avec une date figée, les exemples
// « Example: GET Object » et « Example: PUT Object » de la documentation AWS
// « Signature Calculations for the Authorization Header: Transferring
// Payload in a Single Chunk (AWS Signature Version 4) », récupérée le
// 10 septembre 2026 sur :
// https://docs.aws.amazon.com/AmazonS3/latest/developerguide/sig-v4-header-based-auth.html
//
// Clé d'exemple officielle : AKIAIOSFODNN7EXAMPLE /
// wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY. Les en-têtes et le résultat
// attendu (Authorization, y compris la signature hexadécimale finale) sont
// recopiés tels quels depuis la page ; seule l'intermédiaire "StringToSign"
// n'est pas vérifié ici (nous ne comparons que l'en-tête Authorization final,
// qui suffit à couvrir requête canonique, chaîne à signer et clé de
// signature).

func TestSigner_ExempleOfficielGetObject(t *testing.T) {
	cfg := ConfigS3{
		CleAcces:   "AKIAIOSFODNN7EXAMPLE",
		CleSecrete: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		Region:     "us-east-1",
	}
	date := time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)
	sha256Vide := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	requete, err := http.NewRequest(http.MethodGet, "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	requete.Host = "examplebucket.s3.amazonaws.com"
	requete.Header.Set("Range", "bytes=0-9")
	requete.Header.Set("x-amz-date", "20130524T000000Z")
	requete.Header.Set("x-amz-content-sha256", sha256Vide)

	obtenu := signer(requete, cfg, date, sha256Vide)
	voulu := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request," +
		"SignedHeaders=host;range;x-amz-content-sha256;x-amz-date," +
		"Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"

	if obtenu != voulu {
		t.Errorf("signature GET Object ne correspond pas à l'exemple officiel AWS :\nobtenu %s\nvoulu  %s", obtenu, voulu)
	}
}

func TestSigner_ExempleOfficielPutObject(t *testing.T) {
	cfg := ConfigS3{
		CleAcces:   "AKIAIOSFODNN7EXAMPLE",
		CleSecrete: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		Region:     "us-east-1",
	}
	date := time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)
	sha256Payload := "44ce7dd67c959e0d3524ffac1771dfbba87d2b6b4b4e99e42034a8b803f8b072"

	// L'objet s'appelle "test$file.text" — le '$' doit apparaître encodé
	// (%24) dans le chemin canonique, conformément à l'exemple.
	requete, err := http.NewRequest(http.MethodPut, "https://examplebucket.s3.amazonaws.com/test%24file.text", nil)
	if err != nil {
		t.Fatal(err)
	}
	requete.Host = "examplebucket.s3.amazonaws.com"
	requete.Header.Set("Date", "Fri, 24 May 2013 00:00:00 GMT")
	requete.Header.Set("x-amz-date", "20130524T000000Z")
	requete.Header.Set("x-amz-storage-class", "REDUCED_REDUNDANCY")
	requete.Header.Set("x-amz-content-sha256", sha256Payload)

	obtenu := signer(requete, cfg, date, sha256Payload)
	voulu := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request," +
		"SignedHeaders=date;host;x-amz-content-sha256;x-amz-date;x-amz-storage-class," +
		"Signature=98ad721746da40c64f1a55b78f14c238d841ea1380cd77a1b5971af0ece108bd"

	if obtenu != voulu {
		t.Errorf("signature PUT Object ne correspond pas à l'exemple officiel AWS :\nobtenu %s\nvoulu  %s", obtenu, voulu)
	}
}

// TestSigner_Determinisme vérifie, en complément des exemples officiels
// ci-dessus, que signer est déterministe (même entrée -> même signature) et
// sensible au corps (une empreinte différente change la signature) : deux
// propriétés structurelles indépendantes des valeurs numériques exactes.
func TestSigner_Determinisme(t *testing.T) {
	cfg := ConfigS3{CleAcces: "AKIAEXEMPLE", CleSecrete: "secretexemple", Region: "eu-west-1"}
	date := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	nouvelleRequete := func() *http.Request {
		r, err := http.NewRequest(http.MethodPut, "https://s3.interne.exemple/bucket/cle", nil)
		if err != nil {
			t.Fatal(err)
		}
		r.Host = "s3.interne.exemple"
		r.Header.Set("x-amz-date", "20240115T103000Z")
		r.Header.Set("x-amz-content-sha256", "abc123")
		return r
	}

	a := signer(nouvelleRequete(), cfg, date, "abc123")
	b := signer(nouvelleRequete(), cfg, date, "abc123")
	if a != b {
		t.Errorf("signer non déterministe pour les mêmes entrées : %q != %q", a, b)
	}

	c := signer(nouvelleRequete(), cfg, date, "def456")
	if a == c {
		t.Error("signer ignore l'empreinte du corps : signatures identiques pour deux corps différents")
	}
}

// --- Valider ---

func TestValider(t *testing.T) {
	base := ConfigS3{
		Endpoint:   "https://objets.interne.exemple:9000",
		Bucket:     "bucket",
		CleAcces:   "cle",
		CleSecrete: "secret",
	}

	if err := base.Valider(); err != nil {
		t.Errorf("configuration valide refusée : %v", err)
	}

	sansSchema := base
	sansSchema.Endpoint = "objets.interne.exemple:9000"
	if err := sansSchema.Valider(); err == nil {
		t.Error("endpoint sans schéma http(s) accepté à tort")
	}

	caInexistante := base
	caInexistante.FichierCA = filepath.Join(t.TempDir(), "n-existe-pas.pem")
	if err := caInexistante.Valider(); err == nil {
		t.Error("fichier CA inexistant accepté à tort")
	}

	incomplete := ConfigS3{Endpoint: base.Endpoint}
	if err := incomplete.Valider(); err == nil {
		t.Error("configuration incomplète (bucket et identifiants absents) acceptée à tort")
	}
}

// --- Deposer, contre un serveur de test TLS ---

// ecrireCertificatPEM écrit le certificat du serveur de test dans un fichier
// PEM temporaire — exactement le cas d'usage visé par FichierCA : une
// autorité de certification interne.
func ecrireCertificatPEM(t *testing.T, serveur *httptest.Server) string {
	t.Helper()
	bloc := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serveur.Certificate().Raw})
	chemin := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(chemin, bloc, 0o600); err != nil {
		t.Fatal(err)
	}
	return chemin
}

func ecrireFichierTemporaire(t *testing.T, contenu []byte) string {
	t.Helper()
	chemin := filepath.Join(t.TempDir(), "sauvegarde.db")
	if err := os.WriteFile(chemin, contenu, 0o600); err != nil {
		t.Fatal(err)
	}
	return chemin
}

func TestDeposer_OK(t *testing.T) {
	var methodeRecue, cheminRecu, autorisationRecue, dateRecue, contentSha256Recu string
	var corpsRecu []byte

	serveur := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methodeRecue = r.Method
		cheminRecu = r.URL.Path
		autorisationRecue = r.Header.Get("Authorization")
		dateRecue = r.Header.Get("X-Amz-Date")
		contentSha256Recu = r.Header.Get("X-Amz-Content-Sha256")
		corps, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		corpsRecu = corps
		w.WriteHeader(http.StatusOK)
	}))
	defer serveur.Close()

	contenu := []byte("contenu de test pour le dépôt S3 — plusieurs octets, accentués aussi : éà")
	chemin := ecrireFichierTemporaire(t, contenu)

	cfg := ConfigS3{
		Endpoint:   serveur.URL, // https://127.0.0.1:<port>, style path par défaut
		Bucket:     "bucket",
		Prefixe:    "prefixe",
		CleAcces:   "AKIAEXEMPLE",
		CleSecrete: "secretexemple",
		FichierCA:  ecrireCertificatPEM(t, serveur),
	}

	if err := Deposer(context.Background(), cfg, "cle", chemin); err != nil {
		t.Fatalf("Deposer : erreur inattendue : %v", err)
	}

	if methodeRecue != http.MethodPut {
		t.Errorf("méthode reçue = %q, voulu PUT", methodeRecue)
	}
	if cheminRecu != "/bucket/prefixe/cle" {
		t.Errorf("chemin reçu = %q, voulu /bucket/prefixe/cle", cheminRecu)
	}
	if !strings.HasPrefix(autorisationRecue, "AWS4-HMAC-SHA256 Credential=AKIAEXEMPLE/") ||
		!strings.Contains(autorisationRecue, "/s3/aws4_request,SignedHeaders=") ||
		!strings.Contains(autorisationRecue, ",Signature=") {
		t.Errorf("Authorization de forme inattendue : %q", autorisationRecue)
	}
	if dateRecue == "" {
		t.Error("x-amz-date absent")
	}
	empreinteAttendue := sha256.Sum256(contenu)
	if contentSha256Recu != hex.EncodeToString(empreinteAttendue[:]) {
		t.Errorf("x-amz-content-sha256 = %q, voulu l'empreinte réelle du corps reçu (%x)", contentSha256Recu, empreinteAttendue)
	}
	if !bytes.Equal(corpsRecu, contenu) {
		t.Errorf("corps reçu par le serveur (%d octets) différent du fichier déposé (%d octets)", len(corpsRecu), len(contenu))
	}
}

// TestDeposer_SansCA_Echoue vérifie que, sans FichierCA, la connexion au
// serveur de test échoue (certificat signé par une autorité inconnue du pool
// système) — pas d'option pour contourner la vérification TLS.
func TestDeposer_SansCA_Echoue(t *testing.T) {
	serveur := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer serveur.Close()

	chemin := ecrireFichierTemporaire(t, []byte("contenu"))

	cfg := ConfigS3{
		Endpoint:   serveur.URL,
		Bucket:     "bucket",
		CleAcces:   "AKIAEXEMPLE",
		CleSecrete: "secretexemple",
		// FichierCA volontairement absent.
		Timeout: 5 * time.Second,
	}

	err := Deposer(context.Background(), cfg, "cle", chemin)
	if err == nil {
		t.Fatal("Deposer : attendu une erreur TLS sans FichierCA (certificat inconnu), obtenu nil")
	}
}

func TestDeposer_Erreur403(t *testing.T) {
	corpsErreur := `<?xml version="1.0" encoding="UTF-8"?>` +
		`<Error><Code>AccessDenied</Code><Message>Access Denied</Message></Error>`

	serveur := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(corpsErreur))
	}))
	defer serveur.Close()

	chemin := ecrireFichierTemporaire(t, []byte("contenu"))

	cfg := ConfigS3{
		Endpoint:   serveur.URL,
		Bucket:     "bucket",
		CleAcces:   "AKIAEXEMPLE",
		CleSecrete: "secretexemple",
		FichierCA:  ecrireCertificatPEM(t, serveur),
	}

	err := Deposer(context.Background(), cfg, "cle", chemin)
	if err == nil {
		t.Fatal("Deposer : attendu une erreur pour la réponse 403, obtenu nil")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("erreur ne mentionne pas le code 403 : %v", err)
	}
	if !strings.Contains(err.Error(), "AccessDenied") {
		t.Errorf("erreur ne contient pas le code d'erreur XML AccessDenied : %v", err)
	}
}
