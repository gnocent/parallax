// Package auth porte l'authentification locale (argon2id) derrière
// l'interface Authenticator, et la gestion des sessions côté serveur.
// L'interface existe pour qu'un jour un Authenticator LDAP ou SSO se
// substitue à LocalAuthenticator sans toucher au reste de l'application
// .
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Paramètres argon2id. m est en kilo-octets (64 Mo), t le nombre de passes,
// p le parallélisme — un préréglage « fort » raisonnable pour quelques
// dizaines de comptes, à ajuster si la machine cible est modeste.
const (
	argonMemoire      = 64 * 1024
	argonIterations   = 3
	argonParallelisme = 4
	argonTailleSel    = 16
	argonTailleHash   = 32
)

// HacherMotDePasse produit un hash argon2id au format PHC
// ($argon2id$v=19$m=...,t=...,p=...$sel$hash), autoporteur : les paramètres
// voyagent avec le hash, une évolution future des réglages ne casse pas la
// vérification des hashs existants.
func HacherMotDePasse(motDePasse string) (string, error) {
	if motDePasse == "" {
		return "", fmt.Errorf("mot de passe vide")
	}
	sel := make([]byte, argonTailleSel)
	if _, err := rand.Read(sel); err != nil {
		return "", fmt.Errorf("génération du sel : %w", err)
	}
	hash := argon2.IDKey([]byte(motDePasse), sel, argonIterations, argonMemoire, argonParallelisme, argonTailleHash)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoire, argonIterations, argonParallelisme,
		base64.RawStdEncoding.EncodeToString(sel),
		base64.RawStdEncoding.EncodeToString(hash)), nil
}

// VerifierMotDePasse compare un mot de passe en clair à un hash PHC produit
// par HacherMotDePasse, en temps constant. Une erreur signale un hash mal
// formé (jamais un mot de passe incorrect, qui renvoie simplement false).
func VerifierMotDePasse(hash, motDePasse string) (bool, error) {
	m, t, p, sel, attendu, err := decoderPHC(hash)
	if err != nil {
		return false, err
	}
	obtenu := argon2.IDKey([]byte(motDePasse), sel, t, m, p, uint32(len(attendu)))
	return subtle.ConstantTimeCompare(obtenu, attendu) == 1, nil
}

func decoderPHC(hash string) (m uint32, t uint32, p uint8, sel, empreinte []byte, err error) {
	morceaux := strings.Split(hash, "$")
	// "" $argon2id $v=19 $m=...,t=...,p=... $sel $hash → 6 éléments, le premier vide
	if len(morceaux) != 6 || morceaux[1] != "argon2id" {
		return 0, 0, 0, nil, nil, fmt.Errorf("hash de mot de passe : format PHC invalide")
	}
	var version int
	if _, err := fmt.Sscanf(morceaux[2], "v=%d", &version); err != nil || version != argon2.Version {
		return 0, 0, 0, nil, nil, fmt.Errorf("hash de mot de passe : version argon2 non supportée")
	}
	var pInt int
	if _, err := fmt.Sscanf(morceaux[3], "m=%d,t=%d,p=%d", &m, &t, &pInt); err != nil {
		return 0, 0, 0, nil, nil, fmt.Errorf("hash de mot de passe : paramètres illisibles")
	}
	p = uint8(pInt)
	if sel, err = base64.RawStdEncoding.DecodeString(morceaux[4]); err != nil {
		return 0, 0, 0, nil, nil, fmt.Errorf("hash de mot de passe : sel illisible : %w", err)
	}
	if empreinte, err = base64.RawStdEncoding.DecodeString(morceaux[5]); err != nil {
		return 0, 0, 0, nil, nil, fmt.Errorf("hash de mot de passe : empreinte illisible : %w", err)
	}
	return m, t, p, sel, empreinte, nil
}
