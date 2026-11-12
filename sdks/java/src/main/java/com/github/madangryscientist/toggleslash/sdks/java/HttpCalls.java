package com.github.madangryscientist.toggleslash.sdks.java;

import java.io.IOException;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpConnectTimeoutException;
import java.net.http.HttpHeaders;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.net.http.HttpTimeoutException;
import java.time.Duration;
import java.util.concurrent.CancellationException;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;

/**
 * The three requests the SDK makes. There are two {@code HttpClient}s because JDK 17 cannot close
 * one: the ruleset and stream client has its in-flight request cancelled on {@code close}, and the
 * usage client is left alone so the final flush can still go out. Every request has one deadline,
 * {@code requestTimeout}, that covers connecting, the headers and the whole body.
 */
final class HttpCalls {
  /** What came back: the status, the headers and, where one was read, the body. */
  record Reply(int status, HttpHeaders headers, byte[] body) {}

  private final HttpClient syncClient;
  private final HttpClient usageClient;
  private final String baseUrl;
  private final String sdkKey;
  private final long timeoutMillis;
  private volatile CompletableFuture<?> inFlight;

  HttpCalls(String baseUrl, String sdkKey, Duration requestTimeout) {
    this.baseUrl = baseUrl;
    this.sdkKey = sdkKey;
    this.timeoutMillis = requestTimeout.toMillis();
    this.syncClient = newClient(requestTimeout);
    this.usageClient = newClient(requestTimeout);
  }

  private static HttpClient newClient(Duration connectTimeout) {
    return HttpClient.newBuilder()
        .version(HttpClient.Version.HTTP_1_1)
        .followRedirects(HttpClient.Redirect.NEVER)
        .connectTimeout(connectTimeout)
        .build();
  }

  /** {@code GET /sdk/v1/ruleset}, with {@code If-None-Match} when {@code etag} is not null. */
  Reply getRuleset(String etag) throws SyncException, InterruptedException {
    HttpRequest.Builder builder = request("/sdk/v1/ruleset").GET();
    if (etag != null) {
      builder.header("If-None-Match", etag);
    }
    HttpResponse<byte[]> response =
        await(syncClient, build(builder), HttpResponse.BodyHandlers.ofByteArray(), true);
    return new Reply(response.statusCode(), response.headers(), response.body());
  }

  /** Opens {@code GET /sdk/v1/stream}; the call returns when the headers have arrived. */
  Reply openStream(StreamBody body) throws SyncException, InterruptedException {
    HttpRequest.Builder builder =
        request("/sdk/v1/stream").GET().header("Accept", "text/event-stream");
    HttpResponse<Void> response = await(syncClient, build(builder), info -> body, true);
    return new Reply(response.statusCode(), response.headers(), null);
  }

  /** {@code POST /sdk/v1/events}; the answer's body is discarded. */
  Reply postEvents(byte[] json) throws SyncException, InterruptedException {
    HttpRequest.Builder builder =
        request("/sdk/v1/events")
            .header("Content-Type", "application/json")
            .POST(HttpRequest.BodyPublishers.ofByteArray(json));
    HttpResponse<Void> response =
        await(usageClient, build(builder), HttpResponse.BodyHandlers.discarding(), false);
    return new Reply(response.statusCode(), response.headers(), null);
  }

  /**
   * Cancels the ruleset or stream request in flight, if any: it ends as if the network had dropped.
   */
  void abandon() {
    CompletableFuture<?> future = inFlight;
    if (future != null) {
      future.cancel(true);
    }
  }

  private HttpRequest.Builder request(String path) throws SyncException {
    try {
      return HttpRequest.newBuilder(URI.create(baseUrl + path))
          .header("Authorization", "Bearer " + sdkKey);
    } catch (IllegalArgumentException e) {
      throw new SyncException("network", "the request could not be built", e);
    }
  }

  private static HttpRequest build(HttpRequest.Builder builder) throws SyncException {
    try {
      return builder.build();
    } catch (IllegalArgumentException | IllegalStateException e) {
      throw new SyncException("network", "the request could not be built", e);
    }
  }

  private <T> HttpResponse<T> await(
      HttpClient client, HttpRequest request, HttpResponse.BodyHandler<T> handler, boolean track)
      throws SyncException, InterruptedException {
    CompletableFuture<HttpResponse<T>> future;
    try {
      future = client.sendAsync(request, handler);
    } catch (RuntimeException e) {
      throw new SyncException("network", "the request could not be sent", e);
    }
    if (track) {
      inFlight = future;
    }
    try {
      return future.get(timeoutMillis, TimeUnit.MILLISECONDS);
    } catch (TimeoutException e) {
      future.cancel(true);
      throw new SyncException("timeout", "no answer within " + timeoutMillis + " ms", e);
    } catch (ExecutionException e) {
      Throwable cause = e.getCause();
      if (cause instanceof HttpTimeoutException || cause instanceof HttpConnectTimeoutException) {
        throw new SyncException("timeout", "no answer within " + timeoutMillis + " ms", cause);
      }
      String kind = cause instanceof IOException ? "the connection failed" : "the request failed";
      throw new SyncException("network", kind, cause);
    } catch (CancellationException e) {
      throw new SyncException("network", "the request was abandoned", e);
    } finally {
      if (track) {
        inFlight = null;
      }
    }
  }
}
