package com.github.madangryscientist.toggleslash.sdks.java.testservice;

import com.github.madangryscientist.toggleslash.sdks.java.Client;
import com.github.madangryscientist.toggleslash.sdks.java.Context;
import com.github.madangryscientist.toggleslash.sdks.java.Detail;
import com.github.madangryscientist.toggleslash.sdks.java.EvaluationEvent;
import com.github.madangryscientist.toggleslash.sdks.java.Options;
import com.github.madangryscientist.toggleslash.sdks.java.Status;
import com.github.madangryscientist.toggleslash.sdks.java.evaluation.Reason;
import com.github.madangryscientist.toggleslash.sdks.java.json.Json;
import com.github.madangryscientist.toggleslash.sdks.java.json.Utf8;
import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.time.Duration;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.Executors;
import java.util.concurrent.atomic.AtomicInteger;

/**
 * The SDK conformance test service for the Java SDK: a small HTTP server that holds clients and
 * answers the protocol in docs/sdk-test-service.md. See test-service/README.md.
 */
public final class Main {
  private static final class Held {
    final Client client;
    final List<Json> hookEvents = new CopyOnWriteArrayList<>();
    final List<String> errors = new CopyOnWriteArrayList<>();

    Held(Options options, JsonObject body) {
      boolean hook = body.bool("hook") || body.bool("hookThrows");
      boolean hookThrows = body.bool("hookThrows");
      boolean onErrorThrows = body.bool("onErrorThrows");
      options.onError(error -> {
        errors.add(error.getMessage() == null ? error.getClass().getName() : error.getMessage());
        if (onErrorThrows) {
          throw new IllegalStateException("the onError callback threw, on request");
        }
      });
      if (hook) {
        options.afterEvaluation(event -> {
          hookEvents.add(eventJson(event));
          if (hookThrows) {
            throw new IllegalStateException("the hook threw, on request");
          }
        });
      }
      this.client = Client.create(options);
    }
  }

  /** A thin reader over a request body that is a JSON object. */
  private record JsonObject(Json.Obj obj) {
    boolean bool(String name) {
      return obj.get(name) instanceof Json.Bool b && b.value();
    }

    String string(String name) {
      return obj.get(name) instanceof Json.Str s ? s.value() : null;
    }

    Long millis(String name) {
      return obj.get(name) instanceof Json.Num n ? Long.valueOf((long) n.value()) : null;
    }
  }

  private static final Map<String, Held> CLIENTS = new ConcurrentHashMap<>();
  private static final AtomicInteger NEXT = new AtomicInteger();

  private Main() {}

  /**
   * Starts the service on a free loopback port and prints where it listens.
   *
   * @param args unused
   * @throws IOException when the port cannot be bound
   */
  public static void main(String[] args) throws IOException {
    InetAddress loopback = InetAddress.getByAddress(new byte[] {127, 0, 0, 1});
    HttpServer server = HttpServer.create(new InetSocketAddress(loopback, 0), 0);
    server.setExecutor(
        Executors.newCachedThreadPool(r -> {
          Thread t = new Thread(r, "test-service");
          t.setDaemon(false);
          return t;
        }));
    server.createContext("/", Main::handle);
    server.start();
    System.out.println("listening http://127.0.0.1:" + server.getAddress().getPort());
    System.out.flush();
  }

  private static void handle(HttpExchange exchange) throws IOException {
    try {
      String method = exchange.getRequestMethod();
      String path = exchange.getRequestURI().getPath();
      if (method.equals("GET") && path.equals("/")) {
        respond(
            exchange,
            200,
            Json.of(
                Map.of("name", "toggleslash-java", "capabilities", List.of("integer-accessor"))));
        return;
      }
      if (method.equals("POST") && path.equals("/clients")) {
        create(exchange);
        return;
      }
      String[] parts = path.split("/");
      if (parts.length == 4 && parts[1].equals("clients")) {
        Held held = CLIENTS.get(parts[2]);
        if (held != null && route(exchange, method, parts[3], held)) {
          return;
        }
      }
      respondText(exchange, 404, "no such route");
    } catch (BadRequest e) {
      respondText(exchange, 400, e.getMessage());
    } catch (Throwable t) {
      respondText(exchange, 500, t.toString());
    }
  }

  private static boolean route(HttpExchange exchange, String method, String action, Held held)
      throws IOException {
    switch (method + " " + action) {
      case "POST ready":
        respond(exchange, 200, Json.of(Map.of("ready", held.client.ready())));
        return true;
      case "POST evaluate":
        respond(exchange, 200, evaluate(held.client, readBody(exchange)));
        return true;
      case "GET status":
        respond(exchange, 200, statusJson(held.client.status()));
        return true;
      case "GET hook-events":
        respond(exchange, 200, Json.of(Map.of("events", new ArrayList<Object>(held.hookEvents))));
        return true;
      case "GET errors":
        respond(
            exchange,
            200,
            Json.of(
                Map.of(
                    "count", held.errors.size(), "messages", new ArrayList<Object>(held.errors))));
        return true;
      case "POST close":
        held.client.close();
        respond(exchange, 200, Json.of(Map.of()));
        return true;
      default:
        return false;
    }
  }

  private static void create(HttpExchange exchange) throws IOException {
    JsonObject body = readBody(exchange);
    Options options = new Options();
    options.url(body.string("url")).sdkKey(body.string("sdkKey"));
    if (body.obj().get("stream") instanceof Json.Bool b) {
      options.stream(b.value());
    }
    Long poll = body.millis("pollIntervalMs");
    if (poll != null) {
      options.pollInterval(Duration.ofMillis(poll));
    }
    Long flush = body.millis("flushIntervalMs");
    if (flush != null) {
      options.flushInterval(Duration.ofMillis(flush));
    }
    Long timeout = body.millis("requestTimeoutMs");
    if (timeout != null) {
      options.requestTimeout(Duration.ofMillis(timeout));
    }
    String id = "c" + NEXT.incrementAndGet();
    CLIENTS.put(id, new Held(options, body));
    respond(exchange, 201, Json.of(Map.of("id", id)));
  }

  private static Json evaluate(Client client, JsonObject body) {
    String flagKey = body.string("flagKey");
    String accessor = body.string("accessor");
    Context context = Context.of(body.obj().get("context"));
    Json fallback = body.obj().get("fallback") == null ? Json.NULL : body.obj().get("fallback");
    switch (accessor == null ? "any" : accessor) {
      case "bool":
        return detailJson(
            client.boolVariationDetail(flagKey, context, need(fallback, Json.Bool.class).value()));
      case "string":
        return detailJson(
            client.stringVariationDetail(
                flagKey, context, fallback instanceof Json.Str s ? s.value() : null));
      case "number":
        return detailJson(
            client.doubleVariationDetail(flagKey, context, need(fallback, Json.Num.class).value()));
      case "int":
        return detailJson(
            client.longVariationDetail(
                flagKey, context, (long) need(fallback, Json.Num.class).value()));
      case "json":
      case "any":
        return detailJson(client.jsonVariationDetail(flagKey, context, fallback));
      default:
        throw new BadRequest("unknown accessor " + accessor);
    }
  }

  private static <T extends Json> T need(Json fallback, Class<T> type) {
    if (!type.isInstance(fallback)) {
      throw new BadRequest("a typed accessor needs a fallback of its own kind");
    }
    return type.cast(fallback);
  }

  private static Json detailJson(Detail<?> detail) {
    Map<String, Json> fields = new LinkedHashMap<>();
    fields.put("value", valueJson(detail.value()));
    fields.put("variationIndex", index(detail.variationIndex()));
    fields.put("reason", reasonJson(detail.reason()));
    return new Json.Obj(fields);
  }

  private static Json index(Integer value) {
    return value == null ? Json.NULL : new Json.Num(value);
  }

  /** A value the client handed out, as JSON; a number that is not finite is null. */
  private static Json valueJson(Object value) {
    if (value instanceof Double d && !Double.isFinite(d)) {
      return Json.NULL;
    }
    if (value instanceof Json.Num n && !Double.isFinite(n.value())) {
      return Json.NULL;
    }
    return Json.of(value);
  }

  private static Json reasonJson(Reason reason) {
    Map<String, Json> fields = new LinkedHashMap<>();
    fields.put("kind", new Json.Str(reason.kind().name()));
    if (reason.ruleIndex() != null) {
      fields.put("ruleIndex", new Json.Num(reason.ruleIndex()));
    }
    if (reason.flagKey() != null) {
      fields.put("flagKey", new Json.Str(reason.flagKey()));
    }
    if (reason.code() != null) {
      fields.put("code", new Json.Str(reason.code().name()));
    }
    return new Json.Obj(fields);
  }

  private static Json eventJson(EvaluationEvent event) {
    Map<String, Json> fields = new LinkedHashMap<>();
    fields.put("flagKey", new Json.Str(event.flagKey()));
    fields.put("context", event.context() == null ? Json.NULL : event.context().json());
    fields.put("value", valueJson(event.value()));
    fields.put("variationIndex", index(event.variationIndex()));
    fields.put("reason", reasonJson(event.reason()));
    fields.put("etag", event.etag() == null ? Json.NULL : new Json.Str(event.etag()));
    fields.put(
        "flagVersion", event.flagVersion() == null ? Json.NULL : new Json.Num(event.flagVersion()));
    return new Json.Obj(fields);
  }

  private static Json statusJson(Status status) {
    Map<String, Json> fields = new LinkedHashMap<>();
    fields.put("etag", status.etag() == null ? Json.NULL : new Json.Str(status.etag()));
    fields.put(
        "lastSyncAt",
        status.lastSyncAt() == null ? Json.NULL : new Json.Num(status.lastSyncAt().toEpochMilli()));
    fields.put("stream", new Json.Str(status.stream().value()));
    fields.put(
        "lastError", status.lastError() == null ? Json.NULL : new Json.Str(status.lastError()));
    return new Json.Obj(fields);
  }

  /** A request the service cannot take (not the SDK throwing). */
  private static final class BadRequest extends RuntimeException {
    private static final long serialVersionUID = 1L;

    BadRequest(String message) {
      super(message);
    }
  }

  private static JsonObject readBody(HttpExchange exchange) throws IOException {
    try (InputStream in = exchange.getRequestBody()) {
      Json parsed = Json.parse(in.readAllBytes());
      if (parsed instanceof Json.Obj obj) {
        return new JsonObject(obj);
      }
      throw new BadRequest("the body is not a JSON object");
    }
  }

  private static void respond(HttpExchange exchange, int status, Json body) throws IOException {
    send(exchange, status, "application/json", Utf8.encode(body.toJson()));
  }

  private static void respondText(HttpExchange exchange, int status, String text)
      throws IOException {
    send(exchange, status, "text/plain; charset=utf-8", Utf8.encode(text));
  }

  private static void send(HttpExchange exchange, int status, String type, byte[] bytes)
      throws IOException {
    exchange.getResponseHeaders().set("Content-Type", type);
    exchange.sendResponseHeaders(status, bytes.length == 0 ? -1 : bytes.length);
    try (OutputStream out = exchange.getResponseBody()) {
      out.write(bytes);
    }
  }
}
