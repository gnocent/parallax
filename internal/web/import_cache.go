package web

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

// cacheImport garde en mémoire un fichier importé le temps que l'utilisateur
// passe de « analyser » à « confirmer », sans lui faire re-choisir le
// fichier. Un seul processus, pas de scaling horizontal prévu pour v1 : pas
// besoin de plus qu'une carte protégée par un mutex, purgée par expiration.
//
// Portée volontairement étroite : ce n'est pas un stockage de fichiers
// généraliste, seulement un jeton éphémère entre deux requêtes d'un même
// import.
type cacheImport struct {
	mu      sync.Mutex
	entrees map[string]entreeCacheImport
}

type entreeCacheImport struct {
	contenu []byte
	expire  time.Time
}

var importsEnAttente = &cacheImport{entrees: map[string]entreeCacheImport{}}

const dureeCacheImport = 15 * time.Minute

// Deposer range un contenu et renvoie un jeton opaque à republier tel quel
// dans le formulaire de confirmation.
func (c *cacheImport) Deposer(contenu []byte) (string, error) {
	jeton, err := jetonImport()
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.purgerSansVerrou()
	c.entrees[jeton] = entreeCacheImport{contenu: contenu, expire: time.Now().Add(dureeCacheImport)}
	return jeton, nil
}

// Recuperer renvoie le contenu déposé sous ce jeton, s'il existe et n'a pas
// expiré. Le second retour est false sinon (jeton inconnu, expiré, ou déjà
// consommé — Recuperer ne retire pas l'entrée, Consommer le fait).
func (c *cacheImport) Recuperer(jeton string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entrees[jeton]
	if !ok || time.Now().After(e.expire) {
		return nil, false
	}
	return e.contenu, true
}

// Consommer retire l'entrée après un import réel réussi (ou raté) : un
// jeton ne sert qu'une fois, pour ne pas rejouer accidentellement un import
// déjà confirmé.
func (c *cacheImport) Consommer(jeton string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entrees, jeton)
}

func (c *cacheImport) purgerSansVerrou() {
	maintenant := time.Now()
	for k, e := range c.entrees {
		if maintenant.After(e.expire) {
			delete(c.entrees, k)
		}
	}
}

func jetonImport() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
