# frozen_string_literal: true

require_relative 'clause'
require_relative 'context'
require_relative 'result'
require_relative 'ruleset'

module ToggleSlash
  module Eval
    # Evaluates one flag for one context, to docs/evaluation.md. It has no network code, no JSON and no
    # regular expression. `evaluate` never raises for any ruleset or context: every failure, a prerequisite
    # cycle included, is an ERROR result.
    module Evaluator
      # Prerequisite links from the requested flag; docs/evaluation.md pins the limit.
      MAX_PREREQUISITE_DEPTH = 32

      Run = Struct.new(:ruleset, :contexts, :active, :completed)

      module_function

      def served(variations, index, reason)
        Detail.new(value: variations[index], variation_index: index, reason: reason)
      end

      def off_result(header, reason)
        index = header['offVariation']
        return Detail.new(value: nil, variation_index: nil, reason: reason) if index.nil?

        served(header['variations'], index.to_i, reason)
      end

      def rollout_variation(rollout, position)
        chosen = 0
        cumulative = 0
        rollout['variations'].each do |entry|
          chosen = entry['variation'].to_i
          cumulative += entry['weight']
          break if position < cumulative
        end
        chosen
      end

      def serve_result(flag, serve, contexts, reason)
        rollout = serve['rollout']
        index = if rollout
                  position = Context.kind_bucket(flag['key'], flag['salt'], rollout['contextKind'], contexts)
                  rollout_variation(rollout, position)
                else
                  serve['variation'].to_i
                end
        served(flag['variations'], index, reason)
      end

      def segment_rule_matches?(rule, ready, contexts)
        return false unless rule['clauses'].all? { |clause| Clause.attribute_matches?(clause, contexts) }

        weight = rule['weight']
        return true if weight.nil?

        segment = ready.segment
        Context.kind_bucket(segment['key'], segment['salt'], rule['rolloutContextKind'], contexts) < weight
      end

      def segment_matches?(ready, contexts)
        return false if Rules.lists_context?(ready.excluded, contexts)
        return true if Rules.lists_context?(ready.included, contexts)

        ready.segment['rules'].any? { |rule| segment_rule_matches?(rule, ready, contexts) }
      end

      def clause_matches?(clause, run)
        return Clause.attribute_matches?(clause, run.contexts) unless clause['op'] == 'segmentMatch'

        matched = clause['values'].any? do |key|
          ready = run.ruleset.segments[key]
          !ready.nil? && ready != :malformed && segment_matches?(ready, run.contexts)
        end
        clause['negate'] == true ? !matched : matched
      end

      def prerequisite_met?(prerequisite, result)
        reached = %i[TARGET_MATCH RULE_MATCH FALLTHROUGH].include?(result.reason.kind)
        reached && result.variation_index == prerequisite['variation'].to_i
      end

      def check_prerequisites(flag, run, depth)
        flag['prerequisites'].each do |prerequisite|
          result = evaluate_flag(run, prerequisite['flagKey'], depth + 1)
          return result if result.reason.error?

          unless prerequisite_met?(prerequisite, result)
            reason = Reason.new(kind: :PREREQUISITE_FAILED, flag_key: prerequisite['flagKey'])
            return off_result(flag, reason)
          end
        end
        nil
      end

      def evaluate_entry(entry, run, depth)
        return Eval.failure(:MALFORMED_FLAG) if entry.status == :malformed
        return off_result(entry.header, Reason::OFF) unless entry.header['on']
        return Eval.failure(entry.code) if entry.status == :broken

        flag = entry.flag
        blocked = check_prerequisites(flag, run, depth)
        return blocked if blocked

        targeted = Rules.targeted_variation(entry.targets, run.contexts)
        return served(flag['variations'], targeted, Reason::TARGET_MATCH) unless targeted.nil?

        flag['rules'].each_with_index do |rule, rule_index|
          next unless rule['clauses'].all? { |clause| clause_matches?(clause, run) }

          return serve_result(flag, rule, run.contexts, Reason.new(kind: :RULE_MATCH, rule_index: rule_index))
        end
        serve_result(flag, flag['fallthrough'], run.contexts, Reason::FALLTHROUGH)
      end

      def evaluate_flag(run, key, depth)
        completed = run.completed[key]
        return completed if completed
        return Eval.failure(:PREREQUISITE_CYCLE) if run.active.key?(key)
        return Eval.failure(:PREREQUISITE_DEPTH_EXCEEDED) if depth > MAX_PREREQUISITE_DEPTH

        entry = run.ruleset.flags[key]
        return Eval.failure(:FLAG_NOT_FOUND) if entry.nil?

        run.active[key] = true
        result = evaluate_entry(entry, run, depth)
        run.active.delete(key)
        run.completed[key] = result
        result
      end

      # `ruleset` is a Ruleset built by `Rules.build`, `context` a Hash in either shape of the spec.
      def evaluate(ruleset, flag_key, context)
        contexts = Context.normalise(context)
        return Eval.failure(:INVALID_CONTEXT) if contexts.nil?

        key = flag_key.is_a?(String) ? Text.utf8(flag_key) : flag_key
        evaluate_flag(Run.new(ruleset, contexts, {}, {}), key, 0)
      end
    end
  end
end
