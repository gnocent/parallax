package depot

import (
	"fmt"
	"net/netip"
	"strings"
)

// Adresses IPv4 (backlog v3.1). Stockées en texte dans serveur.ip, plage_ip
// et vlan.derniere_ip, comparées numériquement : « 10.0.0.9 » précède
// « 10.0.0.10 », ce qu'un tri lexical ne donne pas. Le périmètre est IPv4
// seulement (docs/modele-donnees.md §5.3) ; une adresse IPv6 est refusée
// avec un message explicite plutôt que mal interprétée.

// ParserIPv4 convertit une adresse en texte vers sa valeur numérique
// (ordre réseau : 10.0.0.1 -> 0x0A000001). Espaces autour tolérés.
//
// Erreurs : ErrValidation si le texte est vide, n'est pas une IPv4 pointée
// ou est une IPv6.
func ParserIPv4(s string) (uint32, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("%w : adresse IP vide", ErrValidation)
	}
	if strings.Contains(s, ":") {
		return 0, fmt.Errorf("%w : « %s » est une adresse IPv6, hors périmètre (IPv4 seulement)", ErrValidation, s)
	}
	a, err := netip.ParseAddr(s)
	if err != nil || !a.Is4() {
		return 0, fmt.Errorf("%w : « %s » n'est pas une adresse IPv4 (attendu a.b.c.d)", ErrValidation, s)
	}
	o := a.As4()
	return uint32(o[0])<<24 | uint32(o[1])<<16 | uint32(o[2])<<8 | uint32(o[3]), nil
}

// FormaterIPv4 est l'inverse de ParserIPv4 : forme pointée canonique.
func FormaterIPv4(v uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d", byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

// NormaliserIPv4 ramène une adresse en texte à sa forme canonique quand elle
// se parse, et la renvoie recadrée telle quelle sinon. Sert de clé de
// regroupement : deux écritures d'une même adresse doivent tomber dans le
// même seau, et une adresse invalide doit rester visible sous sa forme
// saisie.
func NormaliserIPv4(s string) string {
	if v, err := ParserIPv4(s); err == nil {
		return FormaterIPv4(v)
	}
	return strings.TrimSpace(s)
}

// ComparerIPv4 compare deux adresses numériquement : -1, 0 ou 1.
func ComparerIPv4(a, b uint32) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// IntervalleIPv4 est une plage fermée [Debut, Fin], Debut <= Fin.
type IntervalleIPv4 struct {
	Debut, Fin uint32
}

// ParserIntervalleIPv4 lit un intervalle depuis ses deux bornes en texte et
// vérifie l'ordre des bornes.
//
// Erreurs : ErrValidation si une borne est invalide ou si début > fin.
func ParserIntervalleIPv4(debut, fin string) (IntervalleIPv4, error) {
	d, err := ParserIPv4(debut)
	if err != nil {
		return IntervalleIPv4{}, fmt.Errorf("début de plage : %w", err)
	}
	f, err := ParserIPv4(fin)
	if err != nil {
		return IntervalleIPv4{}, fmt.Errorf("fin de plage : %w", err)
	}
	if d > f {
		return IntervalleIPv4{}, fmt.Errorf("%w : la plage %s–%s a un début après sa fin",
			ErrValidation, FormaterIPv4(d), FormaterIPv4(f))
	}
	return IntervalleIPv4{Debut: d, Fin: f}, nil
}

// Contient indique si l'adresse est dans l'intervalle, bornes comprises.
func (i IntervalleIPv4) Contient(v uint32) bool { return v >= i.Debut && v <= i.Fin }

// Chevauche indique si les deux intervalles ont au moins une adresse en
// commun.
func (i IntervalleIPv4) Chevauche(j IntervalleIPv4) bool {
	return i.Debut <= j.Fin && j.Debut <= i.Fin
}

// Taille est le nombre d'adresses de l'intervalle (uint64 : un intervalle
// couvrant tout l'espace en compte 2^32).
func (i IntervalleIPv4) Taille() uint64 { return uint64(i.Fin) - uint64(i.Debut) + 1 }

// Parcourir appelle fn sur chaque adresse de l'intervalle, dans l'ordre
// croissant, et s'arrête dès que fn renvoie faux. Écrit ainsi pour ne
// jamais déborder un uint32 quand Fin vaut 255.255.255.255.
func (i IntervalleIPv4) Parcourir(fn func(v uint32) bool) {
	for v := i.Debut; ; v++ {
		if !fn(v) || v == i.Fin {
			return
		}
	}
}
