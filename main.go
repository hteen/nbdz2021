package main

import (
	"errors"
	"fmt"
	"github.com/parnurzeal/gorequest"
	"github.com/tidwall/gjson"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

var tokenPath string
var phone string

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("请输入手机号: ")
		fmt.Scanln(&phone)
	} else {
		phone = os.Args[1]
	}

	tokenPath = "token_" + phone + ".json"

	// 首次运行检测登录
	if err := checkLogin(); err != nil {
		log.Println(err.Error())
		if err2 := doLogin(); err2 != nil {
			log.Fatalln(err2.Error())
		}

		if checkLogin() != nil {
			log.Fatalln("登录失败")
		}
	}

	nbdz2021Status()
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()

	for {
		select {
		case <-t.C:
			nbdz2021Status()
		}
	}
}

func checkLogin() error {

	cookie := &http.Cookie{Name: "x-jike-access-token", Value: getAccessToken()}
	request := gorequest.New()
	_, body, _ := request.Post("https://web-api.okjike.com/api/graphql").
		AddCookie(cookie).
		Send(`{"operationName":"UnreadNotification","variables":{},"query":"query UnreadNotification {\n  viewer {\n    unread {\n      systemNotification {\n        unreadCount\n        __typename\n      }\n      notification {\n        unreadCount\n        __typename\n      }\n      __typename\n    }\n    __typename\n  }\n}\n"}`).
		End()

	if !gjson.Get(body, "data.viewer.unread").IsObject() {
		// token刷新失败，
		return refreshToken()
	}

	return nil
}

func doLogin() error {
	// 发送短信
	getSmsCode()
	fmt.Printf("验证码已发送到 " + phone + ", 请输入验证码登录: ")
	var code string
	n, err := fmt.Scanf("%f\n", &code)
	if err != nil || n != 1 {
		log.Fatalln(n, err)
	}

	request := gorequest.New()
	resp, _, _ := request.Post("https://web-api.okjike.com/api/graphql").
		Set("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.114 Safari/537.36").
		Send(`{"operationName":"MixLoginWithPhone","variables":{"smsCode":"` + code + `","mobilePhoneNumber":"` + phone + `","areaCode":"+86"},"query":"mutation MixLoginWithPhone($smsCode: String!, $mobilePhoneNumber: String!, $areaCode: String!) {\n  mixLoginWithPhone(smsCode: $smsCode, mobilePhoneNumber: $mobilePhoneNumber, areaCode: $areaCode) {\n    isRegister\n    user {\n      distinctId: id\n      ...TinyUserFragment\n      __typename\n    }\n    __typename\n  }\n}\n\nfragment TinyUserFragment on UserInfo {\n  avatarImage {\n    thumbnailUrl\n    smallPicUrl\n    picUrl\n    __typename\n  }\n  username\n  screenName\n  briefIntro\n  __typename\n}\n"}`).
		End()

	jsonStr := `{"data":{"refreshToken":{"accessToken":"##","refreshToken":"$$"}}}`
	for _, cookie := range (*http.Response)(resp).Cookies() {
		if cookie.Name == "x-jike-access-token" {
			jsonStr = strings.Replace(jsonStr, "##", cookie.Value, -1)
		}

		if cookie.Name == "x-jike-refresh-token" {
			jsonStr = strings.Replace(jsonStr, "$$", cookie.Value, -1)
		}
	}

	return ioutil.WriteFile(tokenPath, []byte(jsonStr), 0777)
}

func getSmsCode() {
	request := gorequest.New()
	_, body, _ := request.Post("https://web-api.okjike.com/api/graphql").
		Set("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.114 Safari/537.36").
		Send(`{"operationName":"GetSmsCode","variables":{"mobilePhoneNumber":"` + phone + `","areaCode":"+86"},"query":"mutation GetSmsCode($mobilePhoneNumber: String!, $areaCode: String!) {\n  getSmsCode(action: PHONE_MIX_LOGIN, mobilePhoneNumber: $mobilePhoneNumber, areaCode: $areaCode) {\n    action\n    __typename\n  }\n}\n"}`).
		End()

	if gjson.Get(body, "data.getSmsCode.action").String() != "LOGIN" {
		log.Fatalln(gjson.Get(body, "errors.message").String())
	}
}

func nbdz2021Status() {
	if checkLogin() != nil {
		log.Fatalln("登录失败, 需要重新运行")
	}

	request := gorequest.New()
	_, body, _ := request.Get("https://api.ruguoapp.com/1.0/nbdz2021/status").
		Set("x-jike-access-token", getAccessToken()).
		End()
	selfCamp := gjson.Get(body, "selfCamp").String()
	camp := gjson.Get(body, "camp."+selfCamp).Map()
	selfInfo := gjson.Get(body, "self").Map()

	s := []string{
		"我的体力: ", selfInfo["energy"].String(), ", ",
		"待施肥: ", camp["planted"].String(), ", ",
		"总插秧: ", camp["totalPlanted"].String(), ", ",
		"待收割: ", camp["watered"].String(),
	}

	fmt.Printf(strings.Join(s, ""))
	// 默认收割
	act := "REAP"
	actEnergy := uint64(3)

	// 施肥 > 收割 则 施肥
	if camp["planted"].Uint() > camp["watered"].Uint() {
		act = "WATER"
		actEnergy = 2
	}

	// 插秧 < 施肥 则 插秧
	if camp["totalPlanted"].Uint() < camp["planted"].Uint() {
		act = "PLANT"
		actEnergy = 1
	}

	//// 固定施肥
	//act = "WATER"
	//actEnergy = 2

	count := selfInfo["energy"].Uint() / actEnergy

	if count > 1 {
		txt := map[string]string{
			"REAP":  "收割",
			"PLANT": "播种",
			"WATER": "施肥",
		}

		fmt.Printf(", 执行 " + strconv.FormatUint(count, 10) + " 次 " + txt[act])
		nbdz2021Act(act, count)
	} else {
		fmt.Printf(", 体力不足，不执行操作")
	}

	fmt.Printf("\n")
	// 待收割 watered
	// 待施肥 planted
	// 总插秧 totalPlanted

	// 体力 energy
	//"self": {
	//	"energy": 742, 体力
	//		"score": 783, 收割
	//		"planted": 414, 插秧
	//		"watered": 311 施肥
	//},
	//log.Println(body)
}

func nbdz2021Act(act string, count uint64) {
	request := gorequest.New()
	_, _, _ = request.Post("https://api.ruguoapp.com/1.0/nbdz2021/act").
		Set("x-jike-access-token", getAccessToken()).
		// 收割 REAP
		// 播种 PLANT
		// 施肥 WATER
		Send(`{"count": ` + strconv.FormatUint(count, 10) + `,"action": "` + act + `"}`).
		End()
}

func refreshToken() error {
	cookie := &http.Cookie{Name: "x-jike-refresh-token", Value: getRefreshToken()}
	request := gorequest.New()
	_, body, _ := request.Post("https://web-api.okjike.com/api/graphql").
		AddCookie(cookie).
		Send(`{"operationName":"refreshToken","variables":{},"query":"mutation refreshToken {\n  refreshToken {\n    accessToken\n    refreshToken\n  }\n}\n"}`).
		EndBytes()

	if !gjson.GetBytes(body, "data.refreshToken").IsObject() {
		return errors.New("需要重新登录")
	}

	return ioutil.WriteFile(tokenPath, body, 0777)
}

func getAccessToken() string {
	data, _ := ioutil.ReadFile(tokenPath)
	return gjson.GetBytes(data, "data.refreshToken.accessToken").String()
}

func getRefreshToken() string {
	data, _ := ioutil.ReadFile(tokenPath)
	return gjson.GetBytes(data, "data.refreshToken.refreshToken").String()
}
