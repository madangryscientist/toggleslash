# frozen_string_literal: true

module Toggleslash
  # Evaluation to docs/evaluation.md: the evaluator, its numbers, dates, versions, buckets and patterns.
  module Eval
    MAX_DEPTH = 32
    MET = [ReasonKind::TARGET_MATCH, ReasonKind::RULE_MATCH, ReasonKind::FALLTHROUGH].freeze
    ORDER = %w[lt lte gt gte].freeze
    SEMVER = {
      "semverEq" => :zero?.to_proc, "semverLt" => :negative?.to_proc, "semverLte" => ->(c) { c <= 0 },
      "semverGt" => :positive?.to_proc, "semverGte" => ->(c) { c >= 0 }
    }.freeze

    # Clause matching, bucketing and serving: pure functions over a loaded ruleset and a normalised context.
    module Matching
      module_function

      # The value an attribute reads, or nil when it is missing.
      def read(path, kind, context)
        entry = context[kind]
        return nil if entry.nil?

        key, attributes = entry
        first = path[0]
        if first == "key"
          current = key
        elsif attributes && Context.has?(attributes, first)
          current = Context.get(attributes, first)
        else
          return nil
        end
        path.each_with_index do |name, i|
          next if i.zero?
          return nil unless current.is_a?(Hash) && Context.has?(current, name)

          current = Context.get(current, name)
        end
        current
      end

      def compare(op, left, right)
        case op
        when "lt" then left < right
        when "lte" then left <= right
        when "gt" then left > right
        else left >= right
        end
      end

      def compare_text(op, left, right)
        order = Text.compare(left, right)
        compare(op, order, 0)
      end

      def instant_match(op, left, right)
        a = Dates.to_instant(left)
        b = Dates.to_instant(right)
        return false if a.nil? || b.nil?

        op == "before" ? a < b : a > b
      end

      # One candidate against one clause value.
      def match_one(op, cand, val)
        return op == "in" && [true, false].include?(val) && cand == val if [true, false].include?(cand)
        return match_number(op, cand, val) if Num.number?(cand)
        return match_string(op, Text.utf8(cand), val) if cand.is_a?(String)

        false
      end

      def match_number(op, cand, val)
        return false unless Num.number?(val)

        a = Num.to_float(cand)
        b = Num.to_float(val)
        return false unless a.finite? && b.finite?
        return a == b if op == "in"
        return compare(op, a, b) if ORDER.include?(op)

        false
      end

      def match_string(op, cand, val)
        return false unless val.is_a?(String)

        val = Text.utf8(val)
        case op
        when "in" then Text.same?(cand, val)
        when "contains" then Text.contains?(cand, val)
        when "startsWith" then Text.starts_with?(cand, val)
        when "endsWith" then Text.ends_with?(cand, val)
        when "lt", "lte", "gt", "gte" then compare_text(op, cand, val)
        when "matches" then matches?(cand, val)
        else semver_match(op, cand, val)
        end
      end

      def matches?(cand, val)
        prog = Pattern.compile(Text.code_points(val))
        !prog.nil? && Pattern.search(prog, Text.code_points(cand))
      end

      def semver_match(op, cand, val)
        check = SEMVER[op]
        return false if check.nil?

        left = Semver.parse(cand)
        right = Semver.parse(val)
        return false if left.nil? || right.nil?

        check.call(Semver.compare(left, right))
      end

      def attribute_match(clause, context)
        value = read(clause.path, clause.kind, context)
        return false if value.nil?

        candidates = value.is_a?(Array) ? value : [value]
        op = clause.op
        base =
          if %w[before after].include?(op)
            candidates.any? { |a| clause.operands.any? { |v| instant_match(op, a, v) } }
          else
            candidates.any? { |a| clause.operands.any? { |v| match_one(op, a, v) } }
          end
        base != clause.negate
      end

      def bucket_of(key, salt, context, kind)
        entry = context[kind]
        return 0 if entry.nil?

        context_key = kind == Context::DEFAULT_KIND ? entry[0] : "#{kind}:#{entry[0]}"
        Bucket.bucket(key, salt, context_key)
      end

      def serve_index(serve, flag, context)
        return serve.variation unless serve.variation.nil?

        value = bucket_of(flag.key, flag.salt, context, serve.kind)
        total = 0
        serve.weights.each do |variation, weight|
          total += weight
          return variation if value < total
        end
        serve.weights[-1][0] # unreachable: the weights sum to 100000
      end

      def in_keys?(sets, context)
        sets.any? do |kind, keys|
          entry = context[kind]
          !entry.nil? && keys.key?(entry[0])
        end
      end

      def in_segment?(segment, context)
        return false if in_keys?(segment.excluded, context)
        return true if in_keys?(segment.included, context)

        segment.rules.any? do |rule|
          next false unless rule.clauses.all? { |c| attribute_match(c, context) }

          rule.weight.nil? || rule.weight > bucket_of(segment.key, segment.salt, context, rule.kind)
        end
      end

      def clause_matches?(clause, context, segments)
        return attribute_match(clause, context) if clause.segments.nil?

        base = clause.segments.any? do |key|
          segment = segments[key]
          !segment.nil? && in_segment?(segment, context)
        end
        base != clause.negate
      end
    end

    # One evaluation: a context, a ruleset and the prerequisites finished so far.
    class Run
      def initialize(ruleset, context)
        @ruleset = ruleset
        @context = context
        @done = {}
        @active = {}
      end

      def self.error(code)
        [nil, nil, Reason.new(kind: ReasonKind::ERROR, code: code)]
      end

      def enter(key, depth)
        finished = @done[key]
        return finished unless finished.nil?
        return Run.error(ErrorCode::PREREQUISITE_CYCLE) if @active.key?(key)
        return Run.error(ErrorCode::PREREQUISITE_DEPTH_EXCEEDED) if depth > MAX_DEPTH

        entry = @ruleset.flags[key]
        return Run.error(ErrorCode::FLAG_NOT_FOUND) if entry.nil?

        @active[key] = true
        begin
          result = run(entry, depth)
        ensure
          @active.delete(key)
        end
        @done[key] = result
      end

      def run(entry, depth)
        return Run.error(ErrorCode::MALFORMED_FLAG) if entry.status == "malformed" || entry.flag.nil?

        flag = entry.flag
        return off(flag, Reason.new(kind: ReasonKind::OFF)) unless flag.on
        return Run.error(entry.code || ErrorCode::MALFORMED_FLAG) if entry.status == "broken"

        failed = prerequisites(flag, depth)
        return failed unless failed.nil?

        hit = target(flag)
        return [flag.variations[hit], hit, Reason.new(kind: ReasonKind::TARGET_MATCH)] unless hit.nil?

        serve_rules(flag)
      end

      def prerequisites(flag, depth)
        flag.prerequisites.each do |prerequisite_key, required|
          outcome = enter(prerequisite_key, depth + 1)
          kind = outcome[2].kind
          return outcome if kind == ReasonKind::ERROR

          if !MET.include?(kind) || outcome[1] != required
            return off(flag, Reason.new(kind: ReasonKind::PREREQUISITE_FAILED, flag_key: prerequisite_key))
          end
        end
        nil
      end

      def serve_rules(flag)
        segments = @ruleset.segments
        flag.rules.each_with_index do |rule, index|
          next unless rule.clauses.all? { |c| Matching.clause_matches?(c, @context, segments) }

          chosen = Matching.serve_index(rule.serve, flag, @context)
          return [flag.variations[chosen], chosen, Reason.new(kind: ReasonKind::RULE_MATCH, rule_index: index)]
        end
        chosen = Matching.serve_index(flag.fallthrough, flag, @context)
        [flag.variations[chosen], chosen, Reason.new(kind: ReasonKind::FALLTHROUGH)]
      end

      # The variation of the first target, in array order, that matches the context.
      def target(flag)
        best = nil
        flag.targets.each do |kind, by_key|
          entry = @context[kind]
          next if entry.nil?

          hit = by_key[entry[0]]
          best = hit if hit && (best.nil? || hit[1] < best[1])
        end
        best&.first
      end

      def off(flag, reason)
        return [nil, nil, reason] if flag.off.nil?

        [flag.variations[flag.off], flag.off, reason]
      end
    end

    # Loads a parsed ruleset (never fails).
    def self.load_ruleset(raw)
      Loader.load(raw)
    end

    # [value, variation_index, Reason] for one flag and a context. Never raises.
    def self.evaluate(ruleset, flag_key, context)
      normalised = Context.normalise(context)
      return Run.error(ErrorCode::INVALID_CONTEXT) if normalised.nil?

      Run.new(ruleset, normalised).enter(flag_key, 0)
    end
  end
end
