I need you to act as a QA Validator for the omneval project. I have made a ton of progress on the application and my tests are passing, so now it's time to push this platform to it's limits and see what is actually working end to end and what is still broken. 

I have started the docker compose stack by navigating to deploy/docker-compose and running `docker compose up --build`. I need you to take user actions that will test if the platform is working and what is broken. If you find something broken, document what happened, what you expected to happen and use this information to create a github issue with your findings and Acceptance Criteria of what would need to be true for this to be fixed. 

Some actions I would like you to take include: 

1. Use the scripts/qa_validation.py script as necessary to test all python functionality in depth. This is the core functionality of the service so take your time and get this correct. Our python SDK is in sdk/python/omneval_sdk. 
2. Use the similar script for the Golang SDK located at sdk/go.
3. Use the similar script for the Typescript SDK located at sdk/ts.
4. The UI is running on localhost:8002 I have created a dummy user for you `email=admin@omneval.com; password=admin`. You need to log in and examine every page and tab on the website. Use your chrome tools to accomplish this. Document all issues you find as github issues. You should see traces from your previous SDK testing in steps 1-3, so you should see traces in the web ui. Feel free to use any docker cli commands to capture logs from each component if they help you debug and diagnose issues.

The goal of this project is to provide the same functionality as other tools like Helicone and Langfuse provide for:
1. LLM Tracing
2. Prompt Registry
3. Live Evaluations using LLM-as-a-Judges
4. Dashboards for monitoring costs

Pay special attention to the LLM-as-a-Judge functionality, I am not positive this is working and I don't see any code for it in any of the SDKs. Similarly, there is no code for managing prompts in the SDKs either.
