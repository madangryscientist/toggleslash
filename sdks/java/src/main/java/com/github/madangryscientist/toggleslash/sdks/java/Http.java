package com.github.madangryscientist.toggleslash.sdks.java;

import java.io.ByteArrayOutputStream;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.net.http.HttpTimeoutException;
import java.nio.ByteBuffer;
import java.time.Duration;
import java.util.List;
import java.util.Set;
import java.util.concurrent.CancellationException;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.Flow;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;
import java.util.function.Consumer;
import java.util.function.IntPredicate;

/**
 * The SDK's HTTP: one HTTP/1.1 {@link HttpClient}, no redirects, and a deadline that covers headers
 * and body (the JDK's own {@code HttpRequest.timeout} ends at the headers). The response body goes
 * through a {@link HttpResponse.BodySubscriber} that hands itself over at once, so the response
 * future completes when the headers arrive and a long-lived stream does not hold it back.
 */
final class Http {
  /** Why a request did not give an answer. */
  static final class Failure extends Exception {
    private static final long serialVersionUID = 1L;

    final String code;
    final boolean aborted;

    Failure(String code, boolean aborted, Throwable cause) {
      super(code, cause);
      this.code = code;
      this.aborted = aborted;
    }
  }

  /**
   * An answer whose headers have arrived. {@code done} completes when the wanted body has been
   * read.
   */
  record Exchange(int status, String etag, String retryAfter, CompletableFuture<Void> done) {}

  /** One request in flight, so that {@code close} can end it. */
  final class Call {
    final boolean sync;
    volatile boolean aborted;
    private volatile CompletableFuture<?> send;
    private volatile Flow.Subscription subscription;
    private volatile Sink sink;

    Call(boolean sync) {
      this.sync = sync;
    }

    void abort() {
      aborted = true;
      Flow.Subscription s = subscription;
      if (s != null) {
        s.cancel();
      }
      CompletableFuture<?> f = send;
      if (f != null) {
        f.cancel(true);
      }
      Sink k = sink;
      if (k != null) {
        k.done.completeExceptionally(new CancellationException("aborted"));
      }
    }

    void finish() {
      calls.remove(this);
    }
  }

  private final class Sink implements HttpResponse.BodySubscriber<Void> {
    final Call call;
    final Consumer<byte[]> onBytes;
    final CompletableFuture<Void> done = new CompletableFuture<>();

    Sink(Call call, Consumer<byte[]> onBytes) {
      this.call = call;
      this.onBytes = onBytes;
    }

    @Override
    public void onSubscribe(Flow.Subscription s) {
      call.subscription = s;
      if (onBytes == null || call.aborted) {
        s.cancel();
        done.complete(null);
        return;
      }
      s.request(Long.MAX_VALUE);
    }

    @Override
    public void onNext(List<ByteBuffer> items) {
      for (ByteBuffer item : items) {
        byte[] bytes = new byte[item.remaining()];
        item.get(bytes);
        try {
          onBytes.accept(bytes);
        } catch (RuntimeException e) {
          done.completeExceptionally(e);
          call.subscription.cancel();
          return;
        }
      }
    }

    @Override
    public void onError(Throwable error) {
      done.completeExceptionally(error);
    }

    @Override
    public void onComplete() {
      done.complete(null);
    }

    @Override
    public java.util.concurrent.CompletionStage<Void> getBody() {
      return CompletableFuture.completedFuture(null);
    }
  }

  private final HttpClient client =
      HttpClient.newBuilder().version(HttpClient.Version.HTTP_1_1).build();
  private final String base;
  private final String authorization;
  private final Set<Call> calls = ConcurrentHashMap.newKeySet();
  private volatile boolean syncShut;

  Http(String base, String sdkKey) {
    this.base = base;
    this.authorization = "Bearer " + sdkKey;
  }

  /**
   * Starts tracking a request. A sync request begun after {@link #abortSync} is aborted at once.
   */
  Call call(boolean sync) {
    Call call = new Call(sync);
    calls.add(call);
    if (sync && syncShut) {
      call.abort();
    }
    return call;
  }

  /** Ends every ruleset and stream request, now and later. Usage requests are left to finish. */
  void abortSync() {
    syncShut = true;
    for (Call call : calls) {
      if (call.sync) {
        call.abort();
      }
    }
  }

  /** Ends every request. */
  void abortAll() {
    syncShut = true;
    for (Call call : calls) {
      call.abort();
    }
  }

  HttpRequest.Builder request(String path) throws Failure {
    try {
      return HttpRequest.newBuilder(URI.create(base + path)).header("Authorization", authorization);
    } catch (IllegalArgumentException e) {
      throw new Failure("network", false, e);
    }
  }

  /**
   * Sends a request and waits for its headers. The body is read, to {@code onBytes}, only for a
   * status {@code wantBody} accepts; for any other the body is dropped without reading.
   */
  Exchange start(
      Call call,
      HttpRequest request,
      IntPredicate wantBody,
      Consumer<byte[]> onBytes,
      long deadlineNanos)
      throws Failure {
    CompletableFuture<HttpResponse<Void>> response;
    try {
      response =
          client.sendAsync(
              request,
              info -> {
                Sink sink = new Sink(call, wantBody.test(info.statusCode()) ? onBytes : null);
                call.sink = sink;
                return sink;
              });
    } catch (RuntimeException e) {
      throw new Failure("network", call.aborted, e);
    }
    call.send = response;
    if (call.aborted) {
      response.cancel(true);
    }
    HttpResponse<Void> answer = await(call, response, deadlineNanos);
    Sink sink = call.sink;
    if (sink == null) {
      throw new Failure("network", call.aborted, null);
    }
    String etag = answer.headers().firstValue("etag").orElse(null);
    String retryAfter = answer.headers().firstValue("retry-after").orElse(null);
    return new Exchange(answer.statusCode(), etag, retryAfter, sink.done);
  }

  /** Waits for the body of an exchange, within the deadline. */
  void awaitBody(Call call, Exchange exchange, long deadlineNanos) throws Failure {
    await(call, exchange.done(), deadlineNanos);
  }

  /** A request whose whole answer is wanted: headers, then the body only if {@code wantBody}. */
  Result fetch(Call call, HttpRequest request, IntPredicate wantBody, long deadlineNanos)
      throws Failure {
    ByteArrayOutputStream body = new ByteArrayOutputStream();
    Exchange exchange = start(call, request, wantBody, body::writeBytes, deadlineNanos);
    awaitBody(call, exchange, deadlineNanos);
    return new Result(
        exchange.status(), exchange.etag(), exchange.retryAfter(), body.toByteArray());
  }

  /** A finished answer. */
  record Result(int status, String etag, String retryAfter, byte[] body) {}

  private <T> T await(Call call, CompletableFuture<T> future, long deadlineNanos) throws Failure {
    long remaining = Math.max(0L, deadlineNanos - System.nanoTime());
    try {
      return future.get(remaining, TimeUnit.NANOSECONDS);
    } catch (TimeoutException e) {
      call.abort();
      throw new Failure("timeout", false, e);
    } catch (ExecutionException e) {
      Throwable cause = e.getCause();
      if (call.aborted || cause instanceof CancellationException) {
        throw new Failure("network", true, cause);
      }
      if (cause instanceof HttpTimeoutException) {
        throw new Failure("timeout", false, cause);
      }
      throw new Failure("network", false, cause);
    } catch (CancellationException e) {
      throw new Failure("network", true, e);
    } catch (InterruptedException e) {
      call.abort();
      Thread.currentThread().interrupt();
      throw new Failure("network", true, e);
    }
  }

  static long deadline(Duration timeout) {
    return System.nanoTime() + timeout.toNanos();
  }
}
