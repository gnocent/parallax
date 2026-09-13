// Package sauvegarde dépose un fichier local sur un stockage objet compatible
// S3 (MinIO, Ceph, etc.), en HTTPS, sur le réseau interne cloisonné de la
// entreprise, avec une autorité de certification interne.
//
// Aucune dépendance externe : ni le SDK AWS, ni minio-go. Une seule opération
// est nécessaire — PutObject — signée à la main selon AWS Signature Version 4
// (en-tête Authorization, charge utile signée). Référence suivie au plus près
// pour la construction de la requête canonique, de la chaîne à signer et de
// la clé de signature : « Signature Calculations for the Authorization
// Header: Transferring Payload in a Single Chunk (AWS Signature Version 4) »
// https://docs.aws.amazon.com/AmazonS3/latest/developerguide/sig-v4-header-based-auth.html
//
// Les secrets (clé d'accès, clé secrète) viennent de l'environnement — jamais
// en base, la base étant précisément ce qu'on sauvegarde (décision de
// cadrage, voir CLAUDE.md). Le câblage — planification périodique, lecture
// des variables d'environnement, production du fichier à déposer via
// VACUUM INTO — est fait par l'appelant dans cmd/parallax ; ce paquet ne
// fait que le dépôt d'un fichier déjà produit.
package sauvegarde

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

// ConfigS3 décrit la cible du dépôt. Les secrets (CleAcces, CleSecrete)
// viennent de l'environnement du processus (fichier d'environnement systemd
// en 0600) — jamais de la base, précisément parce qu'elle est l'objet de la
// sauvegarde.
type ConfigS3 struct {
	Endpoint     string // https://objets.interne.exemple:9000 (schéma obligatoire)
	Bucket       string
	Prefixe      string // préfixe de clé, ex. "parallax/", peut être vide
	Region       string // "us-east-1" si vide (valeur qu'attendent la plupart des S3 locaux)
	CleAcces     string
	CleSecrete   string
	FichierCA    string        // PEM de l'autorité de certification interne, optionnel
	StyleVirtuel bool          // false = adressage par chemin (défaut, attendu par MinIO et consorts)
	Timeout      time.Duration // 0 = 5 minutes
}

// Configuree indique si Endpoint, Bucket et les deux clés sont renseignés :
// assez pour tenter un dépôt. Ne vérifie pas leur cohérence — voir Valider.
func (c ConfigS3) Configuree() bool {
	return c.Endpoint != "" && c.Bucket != "" && c.CleAcces != "" && c.CleSecrete != ""
}

// Valider vérifie la cohérence de la configuration : présence des champs
// requis, schéma http ou https de l'endpoint, lisibilité et validité du
// fichier CA s'il est fourni. Ne fait aucun accès réseau.
func (c ConfigS3) Valider() error {
	if !c.Configuree() {
		return errors.New("sauvegarde S3 : configuration incomplète (endpoint, bucket et identifiants requis)")
	}
	u, err := url.Parse(c.Endpoint)
	if err != nil {
		return fmt.Errorf("sauvegarde S3 : endpoint %q invalide : %w", c.Endpoint, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("sauvegarde S3 : endpoint %q : schéma http ou https attendu", c.Endpoint)
	}
	if u.Host == "" {
		return fmt.Errorf("sauvegarde S3 : endpoint %q : hôte manquant", c.Endpoint)
	}
	if _, err := chargerPoolCA(c.FichierCA); err != nil {
		return fmt.Errorf("sauvegarde S3 : %w", err)
	}
	return nil
}

// region renvoie la région configurée, ou "us-east-1" par défaut — la valeur
// qu'attendent la plupart des S3 locaux qui n'ont pas de notion de région.
func (c ConfigS3) region() string {
	if c.Region == "" {
		return "us-east-1"
	}
	return c.Region
}

// timeout renvoie le délai configuré, ou 5 minutes par défaut : un fichier de
// sauvegarde peut peser plusieurs centaines de Mo.
func (c ConfigS3) timeout() time.Duration {
	if c.Timeout == 0 {
		return 5 * time.Minute
	}
	return c.Timeout
}

// Deposer envoie le fichier chemin sous la clé cle, préfixée par
// cfg.Prefixe : c'est Deposer qui applique le préfixe (pas l'appelant), pour
// que ConfigS3.Prefixe reste le seul endroit où il est défini — l'appelant ne
// fournit que le nom logique du fichier déposé (ex. le nom du fichier produit
// par VACUUM INTO).
//
// Le corps est haché avant l'envoi (x-amz-content-sha256 réel, jamais
// UNSIGNED-PAYLOAD : le fichier est local, le hachage est possible et plus
// sûr). Le contexte de l'appelant est respecté ; à défaut d'annulation, le
// délai vaut cfg.Timeout (5 minutes si nul).
//
// En cas d'échec, l'erreur renvoyée inclut le code HTTP et le corps de la
// réponse S3 (XML d'erreur).
func Deposer(ctx context.Context, cfg ConfigS3, cle, chemin string) error {
	if err := cfg.Valider(); err != nil {
		return err
	}

	fichier, err := os.Open(chemin)
	if err != nil {
		return fmt.Errorf("dépôt S3 : ouverture de %q : %w", chemin, err)
	}
	defer fichier.Close()

	infos, err := fichier.Stat()
	if err != nil {
		return fmt.Errorf("dépôt S3 : lecture des attributs de %q : %w", chemin, err)
	}

	empreinte := sha256.New()
	if _, err := io.Copy(empreinte, fichier); err != nil {
		return fmt.Errorf("dépôt S3 : calcul de l'empreinte de %q : %w", chemin, err)
	}
	sha256Corps := hex.EncodeToString(empreinte.Sum(nil))
	if _, err := fichier.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("dépôt S3 : retour au début de %q : %w", chemin, err)
	}

	cible, hote, err := construireURL(cfg, cheminCle(cfg, cle))
	if err != nil {
		return err
	}

	requete, err := http.NewRequestWithContext(ctx, http.MethodPut, cible, fichier)
	if err != nil {
		return fmt.Errorf("dépôt S3 : construction de la requête : %w", err)
	}
	requete.ContentLength = infos.Size()
	requete.Host = hote

	maintenant := time.Now().UTC()
	requete.Header.Set("X-Amz-Date", maintenant.Format("20060102T150405Z"))
	requete.Header.Set("X-Amz-Content-Sha256", sha256Corps)
	requete.Header.Set("Authorization", signer(requete, cfg, maintenant, sha256Corps))

	client, err := clientHTTP(cfg)
	if err != nil {
		return err
	}

	reponse, err := client.Do(requete)
	if err != nil {
		return fmt.Errorf("dépôt S3 : requête PUT vers %s : %w", cible, err)
	}
	defer reponse.Body.Close()

	if reponse.StatusCode/100 != 2 {
		corps, _ := io.ReadAll(io.LimitReader(reponse.Body, 64*1024))
		return fmt.Errorf("dépôt S3 : réponse %d %s pour %s : %s",
			reponse.StatusCode, http.StatusText(reponse.StatusCode), cible, string(corps))
	}
	return nil
}

// cheminCle calcule la clé complète : préfixe de configuration suivi de la
// clé fournie par l'appelant, sans double barre oblique.
func cheminCle(cfg ConfigS3, cle string) string {
	prefixe := strings.Trim(cfg.Prefixe, "/")
	cle = strings.TrimPrefix(cle, "/")
	if prefixe == "" {
		return cle
	}
	return prefixe + "/" + cle
}

// construireURL calcule l'URL cible et le nom d'hôte à signer (l'en-tête
// Host), en adressage par chemin par défaut (https://endpoint/bucket/clé,
// ce qu'attendent MinIO et consorts) ou en adressage virtuel-hôte si demandé
// (https://bucket.endpoint/clé).
func construireURL(cfg ConfigS3, cleComplete string) (cible, hote string, err error) {
	base, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return "", "", fmt.Errorf("dépôt S3 : endpoint %q invalide : %w", cfg.Endpoint, err)
	}
	if cfg.StyleVirtuel {
		hote = cfg.Bucket + "." + base.Host
		base.Host = hote
		base.Path = "/" + cleComplete
	} else {
		hote = base.Host
		base.Path = "/" + cfg.Bucket + "/" + cleComplete
	}
	return base.String(), hote, nil
}

// clientHTTP construit le client HTTP : TLS avec le pool de l'autorité de
// certification interne si cfg.FichierCA est renseigné, pool système sinon.
// Aucune option pour désactiver la vérification du certificat : en
// environnement cloisonné, on ne l'offre pas.
func clientHTTP(cfg ConfigS3) (*http.Client, error) {
	pool, err := chargerPoolCA(cfg.FichierCA)
	if err != nil {
		return nil, fmt.Errorf("dépôt S3 : %w", err)
	}
	return &http.Client{
		Timeout: cfg.timeout(),
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool}, // pool nil => pool système
		},
	}, nil
}

// chargerPoolCA charge un fichier PEM d'autorité de certification interne
// dans un pool de certificats. fichier vide (aucune CA fournie) renvoie un
// pool nil sans erreur ; crypto/tls retombe alors sur le pool système.
func chargerPoolCA(fichier string) (*x509.CertPool, error) {
	if fichier == "" {
		return nil, nil
	}
	octetsPEM, err := os.ReadFile(fichier)
	if err != nil {
		return nil, fmt.Errorf("fichier CA %q illisible : %w", fichier, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(octetsPEM) {
		return nil, fmt.Errorf("fichier CA %q : aucun certificat PEM valide", fichier)
	}
	return pool, nil
}

// signer calcule la valeur de l'en-tête Authorization AWS Signature Version 4
// pour req. Elle ne modifie pas req : l'appelant doit avoir positionné au
// préalable req.Host, X-Amz-Date et X-Amz-Content-Sha256, qui font partie des
// en-têtes signés au même titre que tous les autres en-têtes déjà présents
// sur la requête au moment de l'appel.
//
// date fixe l'horodatage utilisé pour le calcul de la portée (scope) et de la
// clé de signature — passé séparément de l'en-tête X-Amz-Date pour permettre
// un test avec une date figée. sha256Corps est l'empreinte SHA-256
// hexadécimale du corps de la requête (jamais UNSIGNED-PAYLOAD ici).
//
// Étapes suivies (voir le commentaire de paquet pour la référence AWS) :
// requête canonique, chaîne à signer, clé de signature dérivée par HMAC-SHA256
// en cascade (date, région, service, "aws4_request"), signature finale.
func signer(req *http.Request, cfg ConfigS3, date time.Time, sha256Corps string) string {
	region := cfg.region()
	dateJour := date.UTC().Format("20060102")
	dateComplete := date.UTC().Format("20060102T150405Z")

	// En-têtes canoniques : host, plus tous les en-têtes déjà présents sur la
	// requête (x-amz-date, x-amz-content-sha256, et tout autre en-tête que
	// l'appelant aurait positionné), triés par nom en minuscules.
	noms := make([]string, 0, len(req.Header)+1)
	noms = append(noms, "host")
	for nom := range req.Header {
		noms = append(noms, strings.ToLower(nom))
	}
	sort.Strings(noms)
	noms = dedoublonner(noms)

	var enTetes strings.Builder
	for _, nom := range noms {
		valeur := req.Header.Get(nom)
		if nom == "host" {
			valeur = req.Host
			if valeur == "" {
				valeur = req.URL.Host
			}
		}
		enTetes.WriteString(nom)
		enTetes.WriteByte(':')
		enTetes.WriteString(strings.TrimSpace(valeur))
		enTetes.WriteByte('\n')
	}
	enTetesSignees := strings.Join(noms, ";")

	requeteCanonique := strings.Join([]string{
		req.Method,
		uriEncode(req.URL.Path, false), // ne pas encoder les "/" du chemin
		canonicalQueryString(req.URL),
		enTetes.String(), // se termine déjà par "\n" : donne la ligne vide requise avant SignedHeaders
		enTetesSignees,
		sha256Corps,
	}, "\n")

	empreinteRequete := sha256.Sum256([]byte(requeteCanonique))
	portee := dateJour + "/" + region + "/s3/aws4_request"
	aSigner := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		dateComplete,
		portee,
		hex.EncodeToString(empreinteRequete[:]),
	}, "\n")

	signature := hex.EncodeToString(hmacSHA256(cleDeSignature(cfg.CleSecrete, dateJour, region), aSigner))

	return "AWS4-HMAC-SHA256 Credential=" + cfg.CleAcces + "/" + portee +
		",SignedHeaders=" + enTetesSignees + ",Signature=" + signature
}

// dedoublonner retire les doublons consécutifs d'une slice déjà triée.
func dedoublonner(noms []string) []string {
	resultat := noms[:0]
	var precedent string
	for i, n := range noms {
		if i == 0 || n != precedent {
			resultat = append(resultat, n)
		}
		precedent = n
	}
	return resultat
}

// hmacSHA256 calcule un HMAC-SHA256.
func hmacSHA256(cle []byte, message string) []byte {
	h := hmac.New(sha256.New, cle)
	h.Write([]byte(message))
	return h.Sum(nil)
}

// cleDeSignature dérive la clé de signature SigV4 par HMAC-SHA256 en cascade :
// date, région, service ("s3"), puis "aws4_request".
func cleDeSignature(cleSecrete, dateJour, region string) []byte {
	cleDate := hmacSHA256([]byte("AWS4"+cleSecrete), dateJour)
	cleRegion := hmacSHA256(cleDate, region)
	cleService := hmacSHA256(cleRegion, "s3")
	return hmacSHA256(cleService, "aws4_request")
}

// uriEncode encode chaque octet sauf les caractères non réservés
// (A-Z a-z 0-9 - _ . ~), conformément aux règles S3 (encodage systématique,
// hexadécimal en majuscules) — les fonctions d'URI-encodage standard des
// langages ne suivent pas exactement ces règles, d'où cette implémentation
// dédiée. encoderBarre commande l'encodage du caractère '/' : faux pour un
// chemin (le "/" séparateur de segments n'est pas encodé), vrai pour une clé
// ou une valeur de paramètre de requête.
func uriEncode(s string, encoderBarre bool) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		case c == '/':
			if encoderBarre {
				b.WriteString("%2F")
			} else {
				b.WriteByte('/')
			}
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// canonicalQueryString construit la chaîne de requête canonique : paramètres
// URI-encodés individuellement, triés par nom puis par valeur. Chaîne vide si
// la requête n'a pas de paramètres.
func canonicalQueryString(u *url.URL) string {
	if u.RawQuery == "" {
		return ""
	}
	valeurs := u.Query()
	noms := make([]string, 0, len(valeurs))
	for nom := range valeurs {
		noms = append(noms, nom)
	}
	sort.Strings(noms)

	var paires []string
	for _, nom := range noms {
		vs := append([]string(nil), valeurs[nom]...)
		sort.Strings(vs)
		for _, v := range vs {
			paires = append(paires, uriEncode(nom, true)+"="+uriEncode(v, true))
		}
	}
	return strings.Join(paires, "&")
}
