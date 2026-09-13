// Package ldap est le client LDAP de Parallax (backlog v3.6) : un bind
// simple LDAPv3 sur TLS, et rien d'autre. Écrit à la main — encodage BER
// des deux messages nécessaires — pour la même raison que le client S3 :
// aucune bibliothèque, un module compilable hors ligne, et une surface
// qu'on peut relire en entier.
//
// Le bind se fait avec l'identité de l'utilisateur (DN construit par
// gabarit) et le mot de passe qu'il vient de saisir : pas de compte de
// service, aucun secret à garder. L'annuaire n'est contacté qu'à une
// connexion, jamais au démarrage.
//
// Références : RFC 4511 (protocole, BindRequest = APPLICATION 0,
// BindResponse = APPLICATION 1), ITU-T X.690 (BER).
package ldap

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
)

// Erreurs métier du bind ; toute autre erreur est une panne (réseau, TLS,
// réponse illisible) et remonte telle quelle, enveloppée.
var (
	// ErrRefus : l'annuaire a répondu, et refuse (invalidCredentials ou tout
	// autre code non nul). Indifférencié volontairement.
	ErrRefus = errors.New("identifiants refusés par l'annuaire")
)

// Config décrit l'annuaire. Vient de l'environnement (docs/deploiement.md
// §6.3), jamais de la base.
type Config struct {
	URL       string        // ldaps://hôte:636 — LDAPS obligatoire
	DNGabarit string        // uid={login},ou=people,dc=exemple,dc=org
	FichierCA string        // PEM de l'autorité interne, optionnel (pool système sinon)
	Delai     time.Duration // connexion et lecture ; 0 = 5 s
}

// Configuree indique si l'authentification LDAP est activée.
func (c Config) Configuree() bool { return strings.TrimSpace(c.URL) != "" }

// Valider vérifie la cohérence de la configuration sans contacter l'annuaire.
func (c Config) Valider() error {
	u, err := url.Parse(strings.TrimSpace(c.URL))
	if err != nil {
		return fmt.Errorf("PARALLAX_LDAP_URL illisible : %w", err)
	}
	if u.Scheme != "ldaps" {
		return fmt.Errorf("PARALLAX_LDAP_URL : seul ldaps:// est accepté (le mot de passe transite vers l'annuaire), obtenu « %s »", u.Scheme)
	}
	if u.Hostname() == "" {
		return errors.New("PARALLAX_LDAP_URL : hôte manquant")
	}
	if !strings.Contains(c.DNGabarit, "{login}") {
		return errors.New("PARALLAX_LDAP_DN : le gabarit doit contenir {login}")
	}
	if c.FichierCA != "" {
		if _, err := chargerPoolCA(c.FichierCA); err != nil {
			return err
		}
	}
	return nil
}

func (c Config) delai() time.Duration {
	if c.Delai <= 0 {
		return 5 * time.Second
	}
	return c.Delai
}

// adresse renvoie hôte:port, 636 par défaut.
func (c Config) adresse() (string, string, error) {
	u, err := url.Parse(strings.TrimSpace(c.URL))
	if err != nil {
		return "", "", err
	}
	port := u.Port()
	if port == "" {
		port = "636"
	}
	return u.Hostname(), net.JoinHostPort(u.Hostname(), port), nil
}

// Lieur est ce que la couche d'authentification consomme : vérifier un
// login et un mot de passe auprès de l'annuaire. Client l'implémente ; les
// tests de l'authentification en substituent un faux.
type Lieur interface {
	Lier(login, motDePasse string) error
}

// Client est le Lieur réel.
type Client struct {
	cfg Config
}

// NouveauClient construit un client sur une configuration validée.
func NouveauClient(cfg Config) (*Client, error) {
	if err := cfg.Valider(); err != nil {
		return nil, err
	}
	return &Client{cfg: cfg}, nil
}

// DN construit le DN de bind d'un login. Les caractères spéciaux d'un DN
// (RFC 4514) sont échappés : un login ne doit pas pouvoir réécrire le
// gabarit.
func (c *Client) DN(login string) string {
	return strings.ReplaceAll(c.cfg.DNGabarit, "{login}", echapperDN(login))
}

// Lier ouvre une connexion TLS, envoie un BindRequest simple et lit le
// BindResponse. Un mot de passe vide est refusé avant tout appel : un bind
// anonyme réussirait sur la plupart des annuaires (RFC 4513 §5.1.2), ce
// qui authentifierait n'importe qui.
func (c *Client) Lier(login, motDePasse string) error {
	login = strings.TrimSpace(login)
	if login == "" || motDePasse == "" {
		return ErrRefus
	}
	hote, adresse, err := c.cfg.adresse()
	if err != nil {
		return fmt.Errorf("annuaire : %w", err)
	}
	pool, err := chargerPoolCA(c.cfg.FichierCA)
	if err != nil {
		return err
	}
	dialer := &net.Dialer{Timeout: c.cfg.delai()}
	conn, err := tls.DialWithDialer(dialer, "tcp", adresse, &tls.Config{ServerName: hote, RootCAs: pool, MinVersion: tls.VersionTLS12})
	if err != nil {
		return fmt.Errorf("annuaire %s : %w", adresse, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(c.cfg.delai()))

	if _, err := conn.Write(EncoderBindRequest(1, c.DN(login), motDePasse)); err != nil {
		return fmt.Errorf("annuaire %s : envoi du bind : %w", adresse, err)
	}
	code, err := LireBindResponse(conn)
	if err != nil {
		return fmt.Errorf("annuaire %s : %w", adresse, err)
	}
	if code != 0 {
		return ErrRefus
	}
	// Unbind (APPLICATION 2, NULL) : courtoisie, la fermeture suffit.
	_, _ = conn.Write([]byte{0x30, 0x05, 0x02, 0x01, 0x02, 0x42, 0x00})
	return nil
}

// ---------------------------------------------------------------- BER

// EncoderBindRequest produit un LDAPMessage complet :
//
//	SEQUENCE {
//	  messageID INTEGER,
//	  [APPLICATION 0] SEQUENCE {
//	    version INTEGER (3),
//	    name OCTET STRING (DN),
//	    authentication [0] OCTET STRING (mot de passe, choix « simple »)
//	  }
//	}
func EncoderBindRequest(messageID int, dn, motDePasse string) []byte {
	bind := concat(
		berEntier(3),
		berChaine(0x04, []byte(dn)),
		berChaine(0x80, []byte(motDePasse)), // [0] contextuel, primitif
	)
	return berSequence(0x30, concat(berEntier(messageID), berSequence(0x60, bind)))
}

// LireBindResponse lit un LDAPMessage et renvoie le resultCode du
// BindResponse ([APPLICATION 1], code 0x61). Un message d'un autre type
// est une erreur de protocole.
func LireBindResponse(r io.Reader) (int, error) {
	message, err := lireElement(r, 0x30)
	if err != nil {
		return 0, fmt.Errorf("réponse illisible : %w", err)
	}
	// messageID
	_, reste, err := decoderElement(message, 0x02)
	if err != nil {
		return 0, fmt.Errorf("réponse illisible : %w", err)
	}
	if len(reste) == 0 {
		return 0, errors.New("réponse illisible : opération absente")
	}
	if reste[0] != 0x61 {
		return 0, fmt.Errorf("réponse inattendue : opération 0x%02x au lieu d'un BindResponse", reste[0])
	}
	corps, _, err := decoderElement(reste, 0x61)
	if err != nil {
		return 0, fmt.Errorf("réponse illisible : %w", err)
	}
	codeOctets, _, err := decoderElement(corps, 0x0a) // ENUMERATED
	if err != nil {
		return 0, fmt.Errorf("réponse illisible : %w", err)
	}
	code := 0
	for _, b := range codeOctets {
		code = code<<8 | int(b)
	}
	return code, nil
}

func concat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// berLongueur encode une longueur en forme courte (< 128) ou longue.
func berLongueur(n int) []byte {
	if n < 128 {
		return []byte{byte(n)}
	}
	var octets []byte
	for v := n; v > 0; v >>= 8 {
		octets = append([]byte{byte(v)}, octets...)
	}
	return append([]byte{0x80 | byte(len(octets))}, octets...)
}

func berChaine(tag byte, v []byte) []byte {
	return concat([]byte{tag}, berLongueur(len(v)), v)
}

func berSequence(tag byte, contenu []byte) []byte {
	return berChaine(tag, contenu)
}

// berEntier encode un entier positif en complément à deux minimal.
func berEntier(v int) []byte {
	var octets []byte
	for {
		octets = append([]byte{byte(v & 0xff)}, octets...)
		v >>= 8
		if v == 0 {
			break
		}
	}
	if octets[0]&0x80 != 0 {
		octets = append([]byte{0}, octets...)
	}
	return berChaine(0x02, octets)
}

// lireElement lit un élément TLV complet depuis r et renvoie son contenu.
func lireElement(r io.Reader, tagAttendu byte) ([]byte, error) {
	entete := make([]byte, 2)
	if _, err := io.ReadFull(r, entete); err != nil {
		return nil, err
	}
	if entete[0] != tagAttendu {
		return nil, fmt.Errorf("tag 0x%02x au lieu de 0x%02x", entete[0], tagAttendu)
	}
	longueur := int(entete[1])
	if entete[1]&0x80 != 0 {
		n := int(entete[1] & 0x7f)
		if n == 0 || n > 4 {
			return nil, errors.New("longueur BER non supportée")
		}
		octets := make([]byte, n)
		if _, err := io.ReadFull(r, octets); err != nil {
			return nil, err
		}
		longueur = 0
		for _, b := range octets {
			longueur = longueur<<8 | int(b)
		}
	}
	if longueur > 1<<16 {
		return nil, errors.New("réponse démesurée")
	}
	contenu := make([]byte, longueur)
	if _, err := io.ReadFull(r, contenu); err != nil {
		return nil, err
	}
	return contenu, nil
}

// decoderElement décode le premier TLV d'un tampon et renvoie (contenu,
// reste).
func decoderElement(b []byte, tagAttendu byte) ([]byte, []byte, error) {
	if len(b) < 2 {
		return nil, nil, errors.New("élément tronqué")
	}
	if b[0] != tagAttendu {
		return nil, nil, fmt.Errorf("tag 0x%02x au lieu de 0x%02x", b[0], tagAttendu)
	}
	longueur, debut := int(b[1]), 2
	if b[1]&0x80 != 0 {
		n := int(b[1] & 0x7f)
		if n == 0 || n > 4 || len(b) < 2+n {
			return nil, nil, errors.New("longueur BER non supportée")
		}
		longueur = 0
		for _, o := range b[2 : 2+n] {
			longueur = longueur<<8 | int(o)
		}
		debut = 2 + n
	}
	if len(b) < debut+longueur {
		return nil, nil, errors.New("élément tronqué")
	}
	return b[debut : debut+longueur], b[debut+longueur:], nil
}

// echapperDN protège un login inséré dans un DN (RFC 4514 §2.4).
func echapperDN(v string) string {
	var b strings.Builder
	for i, r := range v {
		switch r {
		case ',', '+', '"', '\\', '<', '>', ';', '=':
			b.WriteRune('\\')
			b.WriteRune(r)
		case '#':
			if i == 0 {
				b.WriteRune('\\')
			}
			b.WriteRune(r)
		case ' ':
			if i == 0 || i == len(v)-1 {
				b.WriteRune('\\')
			}
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// chargerPoolCA : pool système, ou uniquement l'autorité interne du fichier
// PEM — même comportement que le client S3.
func chargerPoolCA(fichier string) (*x509.CertPool, error) {
	if fichier == "" {
		return nil, nil
	}
	pem, err := os.ReadFile(fichier)
	if err != nil {
		return nil, fmt.Errorf("PARALLAX_LDAP_CA : %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("PARALLAX_LDAP_CA : aucun certificat lisible dans %s", fichier)
	}
	return pool, nil
}
