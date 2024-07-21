# Funnelbase

High performance microservice for rate limiting and caching of outbound requests

## Context
Development of Funnelbase came about due to the need for [Waveous](https://waveous.com/) to limit and optimise requests going to Spotify as the number of distributed services that leaned on Spotify's API increased. Spotify API's rate limits are vague and don't return any meaningful data about number of requests used/remaining for a period of time, only a warning when you need to back off. This makes it complicated for microservices to control the flow of requests out to Spotify's API.

Below is a diagram describing the issue and how Funnelbase fixes this.
![Diagram describing the issue and how Funnelbase fixes this](./assets/context-dark.png)

Scenario 1 is without Funnelbase where X number of services send requests off to an external API whenever they like. This causes 2 issues:
1. How do these microservices manage the flow of requests to an external API, especially if information about the rate limits are not shown?
2. If some of these microservices make the same requests within a period of time that no changes are expected to have occurred on the external API, how can they avoid duplication of these requests?

## Goal
Scenario 2 displays the expected flow of data. X number of services communicate with Funnelbase through gRPC. These gRPC calls contain information about the API request they want to make. As part of this call, a cache lifespan can be specified. If specified, it will check the caching layer (Redis) to see if the response for this API request already exists and will return that instantly, without communicating with the external API. This is useful when requesting [Spotify Artist](https://developer.spotify.com/documentation/web-api/reference/get-an-artist) information for example. There may be multiple services that request the same artist information throughout the day, however this data probably doesn't change very often, so it's worthwhile caching these responses for a period of time.

If no cache exists, no cache lifespan is provided, or it's a POST request, it will proceed to the outbound rate limiter. Rate limits can be set per external API and can limit the throughput of requests to the external API as well as handle back offs if requested by the API. It can also handle multiple priorities, allowing higher priority requests to flow through before lower priority ones.

Once these sequences are completed, the API response data is sent back to the client through gRPC.