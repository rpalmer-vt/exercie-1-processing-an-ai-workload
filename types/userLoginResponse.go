package types

type organization struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

type userLoginDataUserLogin struct {
	Token        string       `json:"token"`
	Organization organization `json:"organization"`
}

type UserLoginData struct {
	UserLogin userLoginDataUserLogin `json:"userLogin"`
}

type UserLoginResponse struct {
	Data UserLoginData `json:"data"`
}
