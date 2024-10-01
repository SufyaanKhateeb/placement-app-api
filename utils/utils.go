package utils

import (
	"regexp"

	"github.com/go-playground/validator/v10"
)

var validate = validator.New()

func GetValidator() *validator.Validate {
	validate.RegisterValidation("password", validatePassword)
	return validate
}

func validatePassword(fl validator.FieldLevel) bool {
	password := fl.Field().String()

	// Check if password contains at least one letter
	hasLetter := regexp.MustCompile(`[a-zA-Z]`).MatchString(password)

	// Check if password contains at least one number
	hasNumber := regexp.MustCompile(`[0-9]`).MatchString(password)

	// Check if password contains at least one special character
	hasSpecialChar := regexp.MustCompile(`[!@#$%^&*(),.?":{}|<>]`).MatchString(password)

	return hasLetter && hasNumber && hasSpecialChar
}
