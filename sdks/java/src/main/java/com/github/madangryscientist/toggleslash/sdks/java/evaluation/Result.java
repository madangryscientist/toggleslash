package com.github.madangryscientist.toggleslash.sdks.java.evaluation;

import com.github.madangryscientist.toggleslash.sdks.java.json.Json;

/**
 * What a flag evaluated to.
 *
 * @param value the served variation's value, or JSON null when nothing is served
 * @param variationIndex the served variation, or null when none was, which tells that apart from a
 *     variation whose value is null
 * @param reason why
 */
public record Result(Json value, Integer variationIndex, Reason reason) {
  static Result failure(ErrorCode code) {
    return new Result(Json.NULL, null, Reason.error(code));
  }
}
