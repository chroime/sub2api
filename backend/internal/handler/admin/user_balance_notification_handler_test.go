package admin

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/balancenotice"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type noticeReader struct{ query balancenotice.Query }
func(r *noticeReader)List(_ context.Context,q balancenotice.Query)(*balancenotice.Page,error){r.query=q;return &balancenotice.Page{Items:[]balancenotice.Record{{ID:1,UserID:7,Recipient:"notify@example.test",Status:"sent"}},Page:q.Page,PageSize:q.PageSize,Total:1,Pages:1,Summary:balancenotice.Summary{Total:1,Sent:1},EvaluatedAt:time.Now()},nil}
func(*noticeReader)Get(_ context.Context,id int64)(*balancenotice.Record,error){if id==404{return nil,balancenotice.ErrNotFound};return &balancenotice.Record{ID:id,UserID:7,Body:"<p>actual notification</p>",Status:"sent"},nil}

func TestBalanceNotificationReadAPIFiltersAndExactBody(t *testing.T){
	gin.SetMode(gin.TestMode);reader:=&noticeReader{};h:=NewUpstreamGovernanceHandler(nil);h.balanceNotifications=reader
	r:=gin.New();r.GET("/notifications",h.ListBalanceNotifications);r.GET("/notifications/:id",h.BalanceNotification)
	for _,url:=range []string{"/notifications?status=bogus","/notifications?trigger_type=astrbot","/notifications?user_id=01","/notifications?user_id=0","/notifications?page=0","/notifications?page_size=101","/notifications?from=bad","/notifications?from=2026-10-27T00:00:00Z&to=2026-10-26T00:00:00Z","/notifications/01","/notifications/-1"}{w:=httptest.NewRecorder();r.ServeHTTP(w,httptest.NewRequest("GET",url,nil));require.Equal(t,400,w.Code,url)}
	w:=httptest.NewRecorder();r.ServeHTTP(w,httptest.NewRequest("GET","/notifications?user_id=7&status=sent&trigger_type=automatic&user_email=user&recipient=notify&page=2&page_size=10",nil))
	require.Equal(t,200,w.Code);require.Equal(t,"no-store",w.Header().Get("Cache-Control"));require.Equal(t,int64(7),reader.query.UserID);require.Equal(t,"notify",reader.query.Recipient);require.Equal(t,2,reader.query.Page);require.NotContains(t,w.Body.String(),`"body"`)
	w=httptest.NewRecorder();r.ServeHTTP(w,httptest.NewRequest("GET","/notifications/1",nil));require.Equal(t,200,w.Code)
	var value struct{Data balancenotice.Record `json:"data"`};require.NoError(t,json.Unmarshal(w.Body.Bytes(),&value));require.Equal(t,"<p>actual notification</p>",value.Data.Body)
	w=httptest.NewRecorder();r.ServeHTTP(w,httptest.NewRequest("GET","/notifications/404",nil));require.Equal(t,404,w.Code)
}
