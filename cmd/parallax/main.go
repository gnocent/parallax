// Commande parallax : serveur unique de l'application.
package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"parallax/internal/auth"
	"parallax/internal/db"
	"parallax/internal/depot"
	"parallax/internal/ldap"
	"parallax/internal/sauvegarde"
	"parallax/internal/web"
)

var version = "dev" // renseigné à la compilation via -ldflags

// dureeArretGracieux borne l'attente de fin des requêtes en cours à l'arrêt
// (SIGTERM systemd, Ctrl+C) avant de couper de force.
const dureeArretGracieux = 10 * time.Second

func main() {
	var (
		chemin      = flag.String("base", envOuDefaut("PARALLAX_BASE", "parallax.db"), "chemin du fichier SQLite (ou PARALLAX_BASE)")
		adresse     = flag.String("adresse", envOuDefaut("PARALLAX_ADRESSE", ":8080"), "adresse d'écoute (ou PARALLAX_ADRESSE)")
		copie       = flag.String("sauvegarde", "", "produit une sauvegarde dans ce fichier puis quitte")
		depotS3     = flag.Bool("sauvegarde-s3", false, "dépose immédiatement une sauvegarde sur le stockage S3 configuré (PARALLAX_S3_*) puis quitte")
		afficherVer = flag.Bool("version", false, "affiche la version et quitte")
	)
	flag.Parse()

	if *afficherVer {
		fmt.Printf("parallax %s\n", version)
		return
	}

	if err := executer(*chemin, *adresse, *copie, *depotS3); err != nil {
		log.Fatalf("parallax : %v", err)
	}
}

// envOuDefaut lit une variable d'environnement, une valeur vide vaut absence
// (on retombe sur defaut) — c'est ce qui permet à un service systemd de
// configurer le binaire par l'environnement plutôt que par des arguments en
// dur dans l'unité (« configuration par fichier ou
// variables d'environnement »). Un flag explicite sur la ligne de commande
// reste prioritaire : flag.String applique defaut seulement si le flag
// n'est pas fourni.
func envOuDefaut(nom, defaut string) string {
	if v := os.Getenv(nom); v != "" {
		return v
	}
	return defaut
}

func executer(chemin, adresse, copie string, depotS3 bool) error {
	if dossier := filepath.Dir(chemin); dossier != "." {
		if err := os.MkdirAll(dossier, 0o750); err != nil {
			return fmt.Errorf("création du répertoire de la base : %w", err)
		}
	}

	base, err := db.Ouvrir(chemin)
	if err != nil {
		return err
	}
	defer base.Close()

	if copie != "" {
		début := time.Now()
		if err := db.Sauvegarder(base, copie); err != nil {
			return err
		}
		log.Printf("sauvegarde écrite dans %s en %s", copie, time.Since(début).Round(time.Millisecond))
		return nil
	}

	cfgS3 := configS3DepuisEnvironnement()
	if depotS3 {
		if !cfgS3.Configuree() {
			return errors.New("dépôt S3 : configuration incomplète — PARALLAX_S3_ENDPOINT, PARALLAX_S3_BUCKET, PARALLAX_S3_CLE_ACCES et PARALLAX_S3_CLE_SECRETE sont requis")
		}
		if err := cfgS3.Valider(); err != nil {
			return fmt.Errorf("dépôt S3 : %w", err)
		}
		return deposerSauvegardeS3(base, cfgS3)
	}

	dépôt := depot.Nouveau(base)
	if err := amorcerCompteAdmin(dépôt); err != nil {
		return fmt.Errorf("amorçage du compte administrateur : %w", err)
	}

	service := auth.NouveauService(dépôt)
	if err := activerLDAP(service, dépôt, configLDAPDepuisEnvironnement()); err != nil {
		return err
	}
	purgerSessionsPériodiquement(service)
	purgerJournalPériodiquement(dépôt)
	planifierSauvegardeS3(base, cfgS3, envOuDefaut("PARALLAX_SAUVEGARDE_INTERVALLE", "24h"))
	planifierSauvegardeLocale(dépôt)

	// derrière un reverse proxy qui termine le TLS, la requête arrive en HTTP
	// simple : l'attribut Secure des cookies doit être forcé.
	web.CookiesSecurises = os.Getenv("PARALLAX_COOKIES_SECURE") == "1"

	serveurHTTP := &http.Server{
		Addr:              adresse,
		Handler:           web.Nouveau(dépôt, service),
		ReadHeaderTimeout: 10 * time.Second,
	}

	return servirAvecArretGracieux(serveurHTTP, version, chemin, adresse)
}

// configLDAPDepuisEnvironnement lit PARALLAX_LDAP_* (docs/deploiement.md
// §6.3). Aucun secret : le bind se fait avec le mot de passe de
// l'utilisateur, jamais avec un compte de service.
func configLDAPDepuisEnvironnement() ldap.Config {
	cfg := ldap.Config{
		URL:       os.Getenv("PARALLAX_LDAP_URL"),
		DNGabarit: os.Getenv("PARALLAX_LDAP_DN"),
		FichierCA: os.Getenv("PARALLAX_LDAP_CA"),
	}
	if v := os.Getenv("PARALLAX_LDAP_DELAI"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.Delai = d
		} else {
			log.Printf("PARALLAX_LDAP_DELAI « %s » illisible, délai par défaut conservé", v)
		}
	}
	return cfg
}

// activerLDAP substitue l'authentificateur combiné (comptes locaux +
// annuaire) à l'authentificateur local quand PARALLAX_LDAP_URL est
// renseignée. La configuration est validée sans contacter l'annuaire :
// pas de dépendance réseau au démarrage. Une configuration incohérente
// empêche le démarrage — mieux qu'un annuaire silencieusement ignoré.
func activerLDAP(service *auth.Service, d *depot.Depot, cfg ldap.Config) error {
	if !cfg.Configuree() {
		return nil
	}
	client, err := ldap.NouveauClient(cfg)
	if err != nil {
		return fmt.Errorf("authentification LDAP : %w", err)
	}
	service.Auth = auth.NouvelAuthenticatorLDAP(d, client)
	log.Printf("authentification LDAP activée vers %s (comptes locaux conservés en secours)", cfg.URL)
	return nil
}

// purgerJournalPériodiquement supprime les entrées du journal au-delà de la
// rétention (depot.RetentionJournal, 800 jours — backlog v3.4) : une fois au
// démarrage, puis chaque jour. Une purge qui échoue est journalisée et
// retentée au tour suivant, jamais bloquante.
func purgerJournalPériodiquement(d *depot.Depot) {
	purger := func() {
		n, err := d.PurgerJournal(depot.RetentionJournal)
		switch {
		case err != nil:
			log.Printf("purge du journal : %v", err)
		case n > 0:
			log.Printf("purge du journal : %d entrée(s) de plus de %d jours supprimée(s)", n, int(depot.RetentionJournal.Hours()/24))
		}
	}
	purger()
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			purger()
		}
	}()
}

// servirAvecArretGracieux démarre le serveur et bloque jusqu'à SIGTERM ou
// SIGINT (Ctrl+C), puis laisse dureeArretGracieux aux requêtes en cours pour
// se terminer avant de couper — un redémarrage systemd (déploiement, rotation
// de logs) ne doit pas trancher une réponse en cours d'écriture.
func servirAvecArretGracieux(serveurHTTP *http.Server, version, chemin, adresse string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	erreurServeur := make(chan error, 1)
	go func() {
		log.Printf("parallax %s — base %s — écoute sur %s", version, chemin, adresse)
		if err := serveurHTTP.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			erreurServeur <- err
			return
		}
		erreurServeur <- nil
	}()

	select {
	case err := <-erreurServeur:
		return err
	case <-ctx.Done():
	}

	log.Printf("arrêt demandé, fin des requêtes en cours (%s maximum)...", dureeArretGracieux)
	ctxArret, annule := context.WithTimeout(context.Background(), dureeArretGracieux)
	defer annule()
	if err := serveurHTTP.Shutdown(ctxArret); err != nil {
		return fmt.Errorf("arrêt du serveur : %w", err)
	}
	log.Printf("arrêt propre terminé")
	return nil
}

// amorcerCompteAdmin crée un compte administrateur avec un mot de passe
// aléatoire imprimé une fois sur la sortie, si aucun compte ADMIN n'existe
// encore — le cas d'une base neuve. Sans ça, personne ne peut se connecter
// à la première installation.
func amorcerCompteAdmin(d *depot.Depot) error {
	utilisateurs, err := d.ListerUtilisateurs(true)
	if err != nil {
		return err
	}
	for _, u := range utilisateurs {
		if u.Role == depot.RoleAdmin {
			return nil
		}
	}

	motDePasse, err := motDePasseAleatoire()
	if err != nil {
		return err
	}
	hash, err := auth.HacherMotDePasse(motDePasse)
	if err != nil {
		return err
	}
	if _, err := d.CreerUtilisateur(depot.Utilisateur{
		Login: "admin", Hash: hash, Role: depot.RoleAdmin,
	}); err != nil {
		return err
	}

	log.Printf("aucun compte administrateur trouvé : compte créé — login « admin », mot de passe « %s »"+
		" — à changer dès la première connexion", motDePasse)
	return nil
}

func motDePasseAleatoire() (string, error) {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("génération du mot de passe initial : %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// configS3DepuisEnvironnement lit la cible S3 (docs/deploiement.md §6.2).
// Les clés viennent de l'environnement — fichier d'environnement systemd en
// 0600 — jamais de la base, qui est précisément ce qu'on sauvegarde.
func configS3DepuisEnvironnement() sauvegarde.ConfigS3 {
	return sauvegarde.ConfigS3{
		Endpoint:     os.Getenv("PARALLAX_S3_ENDPOINT"),
		Bucket:       os.Getenv("PARALLAX_S3_BUCKET"),
		Prefixe:      envOuDefaut("PARALLAX_S3_PREFIXE", "parallax/"),
		Region:       envOuDefaut("PARALLAX_S3_REGION", "us-east-1"),
		CleAcces:     os.Getenv("PARALLAX_S3_CLE_ACCES"),
		CleSecrete:   os.Getenv("PARALLAX_S3_CLE_SECRETE"),
		FichierCA:    os.Getenv("PARALLAX_S3_CA"),
		StyleVirtuel: os.Getenv("PARALLAX_S3_STYLE_VIRTUEL") == "1",
	}
}

// intervalleSauvegarde interprète PARALLAX_SAUVEGARDE_INTERVALLE : une durée
// Go ("24h", "6h30m") ; "0" désactive.
func intervalleSauvegarde(texte string) (time.Duration, error) {
	if texte == "0" || texte == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(texte)
	if err != nil {
		return 0, fmt.Errorf("PARALLAX_SAUVEGARDE_INTERVALLE « %s » illisible : %w", texte, err)
	}
	if d < time.Minute {
		return 0, fmt.Errorf("PARALLAX_SAUVEGARDE_INTERVALLE « %s » : une minute au minimum", texte)
	}
	return d, nil
}

// deposerSauvegardeS3 : VACUUM INTO un fichier temporaire, dépôt sous la clé
// parallax-AAAAMMJJ-HHMMSS.db, suppression du temporaire. La rétention se
// règle sur le bucket (cycle de vie), pas ici.
func deposerSauvegardeS3(base *sql.DB, cfg sauvegarde.ConfigS3) error {
	début := time.Now()
	temporaire := filepath.Join(os.TempDir(), fmt.Sprintf("parallax-sauvegarde-%d.db", début.UnixNano()))
	defer os.Remove(temporaire)
	if err := db.Sauvegarder(base, temporaire); err != nil {
		return err
	}
	cle := "parallax-" + début.UTC().Format("20060102-150405") + ".db"
	ctx, annule := context.WithTimeout(context.Background(), 15*time.Minute)
	defer annule()
	if err := sauvegarde.Deposer(ctx, cfg, cle, temporaire); err != nil {
		return fmt.Errorf("dépôt S3 : %w", err)
	}
	log.Printf("sauvegarde déposée sur %s/%s%s en %s", cfg.Bucket, cfg.Prefixe, cle, time.Since(début).Round(time.Millisecond))
	return nil
}

// planifierSauvegardeS3 lance le dépôt périodique si la cible est configurée.
// Ne bloque jamais le démarrage : une configuration absente ou invalide, ou
// un stockage injoignable, se journalise — principe « pas de dépendance
// réseau au démarrage » — et l'échec est retenté au cycle suivant.
func planifierSauvegardeS3(base *sql.DB, cfg sauvegarde.ConfigS3, intervalleTexte string) {
	if !cfg.Configuree() {
		log.Printf("dépôt S3 périodique désactivé : PARALLAX_S3_* non renseignées")
		return
	}
	intervalle, err := intervalleSauvegarde(intervalleTexte)
	if err != nil {
		log.Printf("dépôt S3 périodique désactivé : %v", err)
		return
	}
	if intervalle == 0 {
		log.Printf("dépôt S3 périodique désactivé : PARALLAX_SAUVEGARDE_INTERVALLE=0")
		return
	}
	if err := cfg.Valider(); err != nil {
		log.Printf("dépôt S3 périodique désactivé : %v", err)
		return
	}
	log.Printf("dépôt S3 périodique toutes les %s vers %s/%s", intervalle, cfg.Bucket, cfg.Prefixe)
	go func() {
		ticker := time.NewTicker(intervalle)
		defer ticker.Stop()
		for range ticker.C {
			if err := deposerSauvegardeS3(base, cfg); err != nil {
				log.Printf("%v — nouvel essai dans %s", err, intervalle)
			}
		}
	}()
}

// heureCorrespond compare l'heure locale de maintenant à cible ("HH:MM") —
// factorisée pour être testable sans dépendre de l'horloge réelle.
func heureCorrespond(maintenant time.Time, cible string) bool {
	return maintenant.Format("15:04") == cible
}

// planifierSauvegardeLocale démarre le planificateur de la sauvegarde locale
// automatique (backlog v3.7, écran /parametres/sauvegarde) : vérifie chaque
// minute si l'heure réglée est atteinte et, si oui, sauvegarde — seulement
// s'il y a eu de l'activité depuis la dernière réussite (depot.ActiviteDepuis)
// — puis purge les copies plus vieilles que la rétention réglée. Les
// réglages sont relus à chaque vérification : les changer depuis l'écran de
// paramètres s'applique sans redémarrage. Désactivé tant qu'aucun dossier ou
// aucune heure n'est réglé — jamais de valeur par défaut choisie à la place
// de l'administrateur.
func planifierSauvegardeLocale(dépôt *depot.Depot) {
	vérifier := func() {
		dossier, _, err := dépôt.LireParametre(depot.ParametreSauvegardeDossier)
		if err != nil {
			log.Printf("sauvegarde locale : %v", err)
			return
		}
		heure, _, err := dépôt.LireParametre(depot.ParametreSauvegardeHeure)
		if err != nil {
			log.Printf("sauvegarde locale : %v", err)
			return
		}
		if dossier == "" || heure == "" {
			return
		}
		maintenant := time.Now()
		if !heureCorrespond(maintenant, heure) {
			return
		}

		// un seul passage par jour, même si l'heure cible reste atteinte
		// pendant plus d'une vérification (démarrage en retard, horloge qui
		// dérive un peu).
		aujourdhui := maintenant.Format("2006-01-02")
		dernièreVérif, _, err := dépôt.LireParametre(depot.ParametreSauvegardeDerniereVerification)
		if err != nil {
			log.Printf("sauvegarde locale : %v", err)
			return
		}
		if dernièreVérif == aujourdhui {
			return
		}

		effectuerSauvegardeLocale(dépôt, dossier)

		if err := dépôt.DefinirParametre(depot.ParametreSauvegardeDerniereVerification, aujourdhui, nil); err != nil {
			log.Printf("sauvegarde locale : enregistrement de la vérification du jour : %v", err)
		}
	}
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			vérifier()
		}
	}()
}

// effectuerSauvegardeLocale écrit une nouvelle sauvegarde si de l'activité a
// eu lieu depuis la précédente réussite (ou qu'il n'y en a jamais eu), puis
// purge les copies plus vieilles que la rétention réglée — dans cet ordre,
// pour que la purge s'applique aussi le jour où rien n'a changé.
func effectuerSauvegardeLocale(dépôt *depot.Depot, dossier string) {
	dernièreRéussiteTexte, présente, err := dépôt.LireParametre(depot.ParametreSauvegardeDerniereReussite)
	if err != nil {
		log.Printf("sauvegarde locale : %v", err)
		return
	}
	actif := !présente
	if présente {
		dernièreRéussite, err := time.Parse(time.RFC3339, dernièreRéussiteTexte)
		if err != nil {
			log.Printf("sauvegarde locale : horodatage de la dernière réussite illisible, sauvegarde forcée : %v", err)
			actif = true
		} else if actif, err = dépôt.ActiviteDepuis(dernièreRéussite); err != nil {
			log.Printf("sauvegarde locale : %v", err)
			return
		}
	}

	if actif {
		if nom, err := sauvegarde.EcrireLocale(dépôt.Base(), dossier); err != nil {
			log.Printf("sauvegarde locale : %v", err)
		} else {
			log.Printf("sauvegarde locale écrite : %s/%s", dossier, nom)
			if err := dépôt.DefinirParametre(depot.ParametreSauvegardeDerniereReussite, time.Now().Format(time.RFC3339), nil); err != nil {
				log.Printf("sauvegarde locale : enregistrement de la réussite : %v", err)
			}
		}
	} else {
		log.Printf("sauvegarde locale : rien de changé depuis la dernière fois, pas de nouveau fichier")
	}

	retentionTexte, _, err := dépôt.LireParametre(depot.ParametreSauvegardeRetentionJours)
	if err != nil {
		log.Printf("sauvegarde locale : %v", err)
		return
	}
	retention, err := strconv.Atoi(retentionTexte)
	if err != nil || retention < 1 {
		return // pas encore réglée correctement : ne purge rien plutôt que de deviner
	}
	if n, err := sauvegarde.PurgerLocales(dossier, time.Duration(retention)*24*time.Hour); err != nil {
		log.Printf("sauvegarde locale : purge : %v", err)
	} else if n > 0 {
		log.Printf("sauvegarde locale : %d fichier(s) de plus de %d jour(s) supprimé(s)", n, retention)
	}
}

// purgerSessionsPériodiquement retire les sessions expirées toutes les
// heures, dans une goroutine détachée. Ce n'est pas critique (Charger purge
// déjà une session expirée à la lecture) : ça évite seulement à la table de
// grossir indéfiniment avec des sessions jamais relues.
func purgerSessionsPériodiquement(service *auth.Service) {
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if err := service.Sessions.Purger(); err != nil {
				log.Printf("purge des sessions : %v", err)
			}
		}
	}()
}
