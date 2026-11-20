package worker

import (
	"testing"
	"time"
)

func TestCalculateNextWorkflowBudget(t *testing.T){day:=24*time.Hour;tests:=[]struct{name string;total,remaining time.Duration;budget int64;current time.Duration;want int64;wantErr bool}{
	{name:"exact seven days",total:7*day,remaining:7*day,budget:70000,current:7*day,want:70000},
	{name:"non-multiple campaign frontloads remainder",total:15*day,remaining:15*day,budget:1500000,current:8*day,want:800000},
	{name:"last workflow receives rounding remainder",total:15*day,remaining:7*day,budget:700001,current:7*day,want:700001},
	{name:"integer currency rounds down proportionally",total:3*day,remaining:3*day,budget:10,current:day,want:3},
	{name:"reject zero duration",total:0,remaining:day,budget:1,current:day,wantErr:true},
	{name:"reject remaining over total",total:7*day,remaining:8*day,budget:1,current:7*day,wantErr:true},
	{name:"reject current over remaining",total:14*day,remaining:7*day,budget:1,current:8*day,wantErr:true},
	{name:"reject negative budget",total:7*day,remaining:7*day,budget:-1,current:7*day,wantErr:true},
};for _,tt:=range tests{t.Run(tt.name,func(t *testing.T){got,err:=CalculateNextWorkflowBudget(tt.total,tt.remaining,tt.budget,tt.current);if (err!=nil)!=tt.wantErr{t.Fatalf("err=%v wantErr=%v",err,tt.wantErr)};if err==nil&&got!=tt.want{t.Fatalf("got %d want %d",got,tt.want)}})}}
