import requests
from collections import Counter

url = "http://775.dustinke.me:30080/"

node1 = "Hello from Node: web-service-pod-1"
node2 = "Hello from Node: web-service-pod-2"

def request_page(url, num_requests=100):
    counter = Counter()
    
    for _ in range(num_requests):
        try:
            response = requests.get(url, timeout=5)
            if response.status_code == 200:
                if node1 in response.text:
                    counter[node1] += 1
                    print(f"Node1: {counter[node1]}")
                elif node2 in response.text:
                    counter[node2] += 1
                    print(f"Node2: {counter[node2]}")
        except requests.exceptions.RequestException as e:
            print(f"Request failed: {e}")
    
    return counter

def calculate_probabilities(counter, num_requests):
    total = sum(counter.values())
    if total == 0:
        print("No successful responses")
        return
    
    print(f"Total requests: {num_requests}")
    print(f"{node1} occurrences: {counter[node1]}, probability: {counter[node1] / total:.2%}")
    print(f"{node2} occurrences: {counter[node2]}, probability: {counter[node2] / total:.2%}")

if __name__ == "__main__":
    num_requests = 100
    counter = request_page(url, num_requests)
    calculate_probabilities(counter, num_requests)
