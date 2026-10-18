import { request, defaults, observe } from "noe:agent/internal";
import { createAgentAPI } from "noe:agent/api";

const api = createAgentAPI({ request, defaults, observe });

export const run_agent = api.run_agent;
export const spawn_agent = api.spawn_agent;
export const observe_agent = api.observe_agent;
export const queue_agent = api.queue_agent;
export const steer_agent = api.steer_agent;
export const interrupt_agent = api.interrupt_agent;
export const list_models = api.list_models;
