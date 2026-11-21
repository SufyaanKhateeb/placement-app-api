package service

type ServiceError struct {
	Err        error
	StatusCode int
}

func (e *ServiceError) Error() string {
	return e.Err.Error()
}
