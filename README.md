# Spark

Spark is a lightweight agent harness, designed to only have read-only tool
calls.

This is for two reasons:
    - Human Context: Software engineers NEED to remain in the loop of the
      output, for that there is no better solution than actually doing this
      work. LLMs are very good at analysis and actually making changes however,
      if enough changes are made to a system that no one understands it anymore,
      it becomes increasingly difficult to know what to ask or how to direct
      said LLM.
    - Improved security, if the LLM does not have the ability to compromise the
      system then that's great.

Mainly, spark is intended to be used with a lightweight (targetting Qwen 3.5 2B
or 4B Q5) LLM. It's a tradeoff where we want this to run locally on consumer
laptops (DDR5+ iGPU/CPU based) while still being capable enough to run analysis
as required.

I'm already aware that both 2B and 4B struggle with questions around libraries
in Clojure, so YMMV but we are targetting:

- Golang
- Java
- Python
- Typescript
- Javascript
- Bash
