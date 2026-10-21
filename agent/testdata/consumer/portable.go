package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"forge.harakara.site/littleisland/noe/v2/agent/auth"
	agentlog "forge.harakara.site/littleisland/noe/v2/agent/log"
	"forge.harakara.site/littleisland/noe/v2/agent/model"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

type portableProvider struct{}

func (portableProvider) Generate(context.Context, model.Request) (model.Response, error) {
	return model.Response{Message: json.RawMessage(`{"role":"assistant","content":"standalone"}`), FinishReason: "stop"}, nil
}
func runPortable() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	service, err := model.NewService(model.Options{Provider: portableProvider{}})
	if err != nil {
		return err
	}
	data := []byte("{\"executionId\":\"fixture\",\"seq\":1,\"type\":\"agent.completed\",\"runId\":\"r\"}\n")
	if s, err := agentlog.AnalyzeBytes(ctx, data, agentlog.Query{RunID: "r"}); err != nil || s.Status != "completed" {
		return fmt.Errorf("Go log snapshot=%+v err=%v", s, err)
	}
	_, err = stdlib.Execute(ctx, stdlib.Source{Name: "[independent bindings]", Virtual: true, Module: true, Code: `
import {generate,list_models} from "noe:model";
import {save_chatgpt_credentials} from "noe:auth";
import {analyze_log} from "noe:agent-log";
if(typeof save_chatgpt_credentials!=="function")throw Error("auth");
const response=await generate({provider:"custom",model:"fixture",messages:[{role:"user",content:"hello"}]});
if(response.message.content!=="standalone"||!Array.isArray(await list_models()))throw Error("model");
if(analyze_log(logData,{runId:"r"}).status!=="completed")throw Error("log");
`}, stdlib.Profile{Initialize: func(ctx context.Context, b *stdlib.Bindings) (func(), error) {
		a, err := auth.Bind(ctx, b, auth.Options{})
		if err != nil {
			return nil, err
		}
		l, err := agentlog.Bind(ctx, b)
		if err != nil {
			a.Close()
			return nil, err
		}
		m, err := model.Bind(ctx, b, service)
		if err != nil {
			l.Close()
			a.Close()
			return nil, err
		}
		if err = b.Runtime().Set("logData", string(data)); err != nil {
			m.Close()
			l.Close()
			a.Close()
			return nil, err
		}
		return func() { m.Close(); l.Close(); a.Close() }, nil
	}})
	return err
}
