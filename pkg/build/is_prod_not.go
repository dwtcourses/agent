// +build !prod

package build

func IsProduction() bool {
	return false
}
