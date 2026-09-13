package web

import (
	"context"

	"parallax/internal/auth"
)

// Clés de contexte privées au paquet : impossible à lire ou écrire de
// l'extérieur, comme le veut l'idiome context.
type cle int

const (
	cleIdentite cle = iota
	cleSession
)

func avecIdentite(ctx context.Context, i auth.Identite) context.Context {
	return context.WithValue(ctx, cleIdentite, i)
}

// identiteDepuis renvoie l'utilisateur de la requête courante, s'il y en a
// une (chargerSession l'a posée après vérification de la session). Le second
// retour est false pour un visiteur non connecté.
func identiteDepuis(ctx context.Context) (auth.Identite, bool) {
	i, ok := ctx.Value(cleIdentite).(auth.Identite)
	return i, ok
}

func avecSession(ctx context.Context, s auth.Session) context.Context {
	return context.WithValue(ctx, cleSession, s)
}

func sessionDepuis(ctx context.Context) (auth.Session, bool) {
	s, ok := ctx.Value(cleSession).(auth.Session)
	return s, ok
}
