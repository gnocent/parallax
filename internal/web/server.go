package web

import (
	"html/template"
	"net/http"

	"parallax/internal/auth"
	"parallax/internal/depot"
)

// serveur porte les dépendances partagées par tous les écrans. Non exporté :
// l'unique point d'entrée du paquet est Nouveau.
type serveur struct {
	mux      *http.ServeMux
	gabarits *template.Template
	depot    *depot.Depot
	auth     *auth.Service
}

// Nouveau construit le handler HTTP complet de Parallax : routage,
// gabarits embarqués, middlewares (session, journalisation, récupération de
// panique). d et svc doivent être prêts à l'emploi (base migrée).
func Nouveau(d *depot.Depot, svc *auth.Service) http.Handler {
	s := &serveur{
		mux:      http.NewServeMux(),
		gabarits: chargerGabarits(),
		depot:    d,
		auth:     svc,
	}
	s.routes()
	return recuperer(journaliser(s.chargerSession(s.mux)))
}

// routes enregistre les routes communes puis délègue à une fonction routesXxx
// par groupe d'écrans, définie dans son propre fichier handlers_*.go. Cette
// fonction est le seul endroit où l'intégration ajoute l'appel à un nouveau
// groupe : les fichiers d'écran ne la modifient jamais, pour éviter tout
// conflit entre travaux menés en parallèle.
func (s *serveur) routes() {
	statique := http.FileServerFS(statiqueRacine())
	s.mux.Handle("GET /statique/", http.StripPrefix("/statique/", statique))

	// Pas d'authentification : sondée par un répartiteur de charge ou
	// systemd (ExecStartPost, health check), qui n'a pas de session.
	s.mux.HandleFunc("GET /sante", s.sante)

	s.mux.HandleFunc("GET /connexion", s.connexionFormulaire)
	s.mux.HandleFunc("POST /connexion", s.connexionSoumettre)
	s.mux.HandleFunc("POST /deconnexion", s.exigerAuth(s.verifierCSRF(s.deconnexionSoumettre)))
	s.mux.HandleFunc("GET /", s.accueil)
	s.routesLangue() // accessible sans connexion : bascule visible dès l'écran de connexion

	s.routesProjets() // patron de référence, voir handlers_projet.go
	s.routesTechnos()
	s.routesEnvironnements()
	s.routesTiers()
	s.routesUsages()
	s.routesZones()
	s.routesModeles()
	s.routesClusters()
	s.routesServeurs()
	s.routesDimensionnement()
	s.routesDimensionnementInverse()
	s.routesContraintes()
	s.routesComparaison()
	s.routesVues()
	s.routesUtilisateurs()
	s.routesImport()
	s.routesExemples()
	s.routesImportReferentiels()
	s.routesImportClusters()
	s.routesImportModeles()
	s.routesImportServeurs()
	s.routesImportMaj()
	s.routesMetriques()
	s.routesVariables()
	s.routesRegles()
	s.routesScenarios()
	s.routesScenarioSynthese()
	s.routesCapacite()
	// v3
	s.routesDemandes()
	s.routesParametres()
	s.routesSauvegarde()
	s.routesLicences()
	s.routesVlans()
	s.routesImportVlans()
	s.routesAdressage()
	s.routesJournal()
}

// sante vérifie que la base répond et renvoie 200 texte brut si c'est le cas,
// 503 sinon. Délibérément minimal — pas de détail interne exposé sans
// authentification.
func (s *serveur) sante(w http.ResponseWriter, r *http.Request) {
	if err := s.depot.Base().PingContext(r.Context()); err != nil {
		http.Error(w, "indisponible", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("ok"))
}

// accueil redirige vers le premier écran utile. Pas de tableau de bord dédié
// en v1 : le référentiel projets sert de page d'atterrissage.
func (s *serveur) accueil(w http.ResponseWriter, r *http.Request) {
	if _, ok := identiteDepuis(r.Context()); !ok {
		http.Redirect(w, r, "/connexion", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/projets", http.StatusSeeOther)
}
